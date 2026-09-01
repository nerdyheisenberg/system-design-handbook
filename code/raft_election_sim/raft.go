// Package main simulates Raft leader election. See Chapter 21.
//
// This is the election half of Raft only — no log replication — because election
// is where the interesting safety argument lives:
//
//   - A candidate needs votes from a MAJORITY. Two majorities of the same cluster
//     must intersect, and a node votes at most once per term, so two leaders
//     cannot both be elected in the same term. That single argument is why split
//     brain is impossible.
//   - Terms are a logical clock. Any message carrying a higher term immediately
//     demotes the receiver to follower, which is how a partitioned old leader
//     steps down the moment it reconnects.
//   - Election timeouts are RANDOMISED. Without that, candidates repeatedly split
//     the vote in lockstep and no one ever wins — the same jitter argument as
//     retry backoff.
//
// FLP says no deterministic consensus algorithm can guarantee termination in an
// asynchronous system. Raft escapes it exactly here: randomised timeouts make
// termination probabilistic rather than guaranteed. It does not violate FLP; it
// sidesteps it.
package main

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

func (r Role) String() string {
	switch r {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	}
	return "unknown"
}

type Node struct {
	mu sync.Mutex

	ID   int
	role Role
	// term is Raft's logical clock. It only ever increases.
	term int
	// votedFor is -1 when this node has not voted in the current term. Resetting
	// it on every term change is what allows one vote per node per term.
	votedFor int

	cluster *Cluster
	// electionDeadline is when this node gives up waiting for a leader.
	electionDeadline time.Time
	votesReceived    map[int]bool
}

type Cluster struct {
	mu    sync.Mutex
	nodes map[int]*Node
	// partitioned nodes drop all messages, simulating a network split.
	partitioned map[int]bool
	rng         *rand.Rand
	now         time.Time
	log         []string

	minTimeout time.Duration
	maxTimeout time.Duration
	tick       time.Duration
}

func NewCluster(size int, seed int64) *Cluster {
	c := &Cluster{
		nodes:       make(map[int]*Node, size),
		partitioned: make(map[int]bool),
		rng:         rand.New(rand.NewSource(seed)),
		now:         time.Unix(0, 0),
		minTimeout:  150 * time.Millisecond,
		maxTimeout:  300 * time.Millisecond,
		tick:        10 * time.Millisecond,
	}
	for i := 0; i < size; i++ {
		n := &Node{ID: i, role: Follower, votedFor: -1, cluster: c}
		n.resetElectionTimer()
		c.nodes[i] = n
	}
	return c
}

// majority is floor(n/2)+1. Any two majorities of the same set must overlap in at
// least one node, which is the entire safety argument for single leadership.
func (c *Cluster) majority() int { return len(c.nodes)/2 + 1 }

// resetElectionTimer picks a randomised deadline. ⭐ The randomisation is not an
// optimisation: with identical timeouts, every follower becomes a candidate
// simultaneously, splits the vote, and the cluster livelocks.
func (n *Node) resetElectionTimer() {
	c := n.cluster
	spread := c.maxTimeout - c.minTimeout
	d := c.minTimeout + time.Duration(c.rng.Int63n(int64(spread)))
	n.electionDeadline = c.now.Add(d)
}

func (c *Cluster) logf(format string, args ...any) {
	c.log = append(c.log, fmt.Sprintf("[%6dms] ", c.now.UnixMilli())+fmt.Sprintf(format, args...))
}

// Step advances the simulated clock by one tick and runs whatever is due.
func (c *Cluster) Step() {
	c.mu.Lock()
	c.now = c.now.Add(c.tick)
	c.mu.Unlock()

	ids := c.nodeIDs()

	// Leaders send heartbeats, which suppress elections.
	for _, id := range ids {
		n := c.nodes[id]
		n.mu.Lock()
		isLeader := n.role == Leader
		n.mu.Unlock()
		if isLeader && !c.isPartitioned(id) {
			c.sendHeartbeats(n)
		}
	}

	// Followers and candidates whose timers expired start a new election.
	for _, id := range ids {
		n := c.nodes[id]
		n.mu.Lock()
		expired := n.role != Leader && !c.now.Before(n.electionDeadline)
		n.mu.Unlock()
		if expired {
			c.startElection(n)
		}
	}
}

func (c *Cluster) nodeIDs() []int {
	ids := make([]int, 0, len(c.nodes))
	for id := range c.nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (c *Cluster) startElection(n *Node) {
	n.mu.Lock()
	n.term++
	n.role = Candidate
	n.votedFor = n.ID // a candidate always votes for itself
	n.votesReceived = map[int]bool{n.ID: true}
	term := n.term
	n.resetElectionTimer()
	n.mu.Unlock()

	c.logf("node %d starts election for term %d", n.ID, term)

	if c.isPartitioned(n.ID) {
		// It can campaign, but no votes reach it. It will never win, which is
		// exactly right: a minority partition must not elect a leader.
		return
	}

	votes := 1
	for _, id := range c.nodeIDs() {
		if id == n.ID || c.isPartitioned(id) {
			continue
		}
		if c.requestVote(c.nodes[id], term, n.ID) {
			votes++
		}
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	// Re-check: a higher term may have demoted us while collecting votes.
	if n.role == Candidate && n.term == term && votes >= c.majority() {
		n.role = Leader
		c.logf("node %d becomes LEADER for term %d with %d/%d votes",
			n.ID, term, votes, len(c.nodes))
	}
}

// requestVote grants a vote if the candidate's term is at least ours and we have
// not already voted this term.
func (c *Cluster) requestVote(voter *Node, term, candidateID int) bool {
	voter.mu.Lock()
	defer voter.mu.Unlock()

	if term < voter.term {
		return false // stale candidate
	}
	if term > voter.term {
		// A higher term always demotes, even a leader.
		voter.term = term
		voter.role = Follower
		voter.votedFor = -1
	}
	// ⭐ One vote per node per term. This is what makes two majorities impossible.
	if voter.votedFor != -1 && voter.votedFor != candidateID {
		return false
	}
	voter.votedFor = candidateID
	voter.resetElectionTimer()
	return true
}

// sendHeartbeats suppresses elections in followers that can hear the leader.
func (c *Cluster) sendHeartbeats(leader *Node) {
	leader.mu.Lock()
	term := leader.term
	leader.mu.Unlock()

	for _, id := range c.nodeIDs() {
		if id == leader.ID || c.isPartitioned(id) {
			continue
		}
		f := c.nodes[id]
		f.mu.Lock()
		if term >= f.term {
			if term > f.term {
				f.term = term
				f.votedFor = -1
			}
			f.role = Follower
			f.resetElectionTimer()
		} else {
			// The "leader" is stale; it will discover this and step down.
			staleTerm := f.term
			f.mu.Unlock()
			leader.mu.Lock()
			if staleTerm > leader.term {
				leader.term = staleTerm
				leader.role = Follower
				leader.votedFor = -1
				c.logf("node %d steps down: saw term %d", leader.ID, staleTerm)
			}
			leader.mu.Unlock()
			continue
		}
		f.mu.Unlock()
	}
}

func (c *Cluster) isPartitioned(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.partitioned[id]
}

// Partition isolates a set of nodes from the rest of the cluster.
func (c *Cluster) Partition(ids ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		c.partitioned[id] = true
	}
	c.logf("partitioned nodes %v", ids)
}

func (c *Cluster) Heal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partitioned = map[int]bool{}
	c.logf("partition healed")
}

// Leaders returns the current leaders. ⭐ Across a single term this must never
// exceed one.
func (c *Cluster) Leaders() []int {
	var out []int
	for _, id := range c.nodeIDs() {
		n := c.nodes[id]
		n.mu.Lock()
		if n.role == Leader {
			out = append(out, id)
		}
		n.mu.Unlock()
	}
	return out
}

// LeadersByTerm groups current leaders by their term.
func (c *Cluster) LeadersByTerm() map[int][]int {
	out := map[int][]int{}
	for _, id := range c.nodeIDs() {
		n := c.nodes[id]
		n.mu.Lock()
		if n.role == Leader {
			out[n.term] = append(out[n.term], id)
		}
		n.mu.Unlock()
	}
	return out
}

func (c *Cluster) Term(id int) int {
	n := c.nodes[id]
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.term
}

func (c *Cluster) Role(id int) Role {
	n := c.nodes[id]
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.role
}

// RunUntilLeader steps the cluster until a leader emerges or the budget expires.
func (c *Cluster) RunUntilLeader(maxTicks int) (int, bool) {
	for i := 0; i < maxTicks; i++ {
		c.Step()
		if l := c.Leaders(); len(l) == 1 {
			return l[0], true
		}
	}
	return -1, false
}

func (c *Cluster) Log() []string { return c.log }

func main() {
	fmt.Println("=== normal election, 5 nodes ===")
	c := NewCluster(5, 1)
	leader, ok := c.RunUntilLeader(200)
	for _, line := range c.Log() {
		fmt.Println(" ", line)
	}
	fmt.Printf("elected node %d (ok=%v), majority is %d of 5\n\n", leader, ok, c.majority())

	fmt.Println("=== partition: 2 nodes isolated from 3 ===")
	c2 := NewCluster(5, 7)
	c2.RunUntilLeader(200)
	before := c2.Leaders()

	// Isolate the minority. It must NOT elect a leader.
	c2.Partition(3, 4)
	for i := 0; i < 200; i++ {
		c2.Step()
	}

	fmt.Printf("leader before partition: %v\n", before)
	fmt.Printf("leaders after partition: %v\n", c2.Leaders())
	fmt.Printf("node 3 role=%s term=%d (kept campaigning, never won)\n",
		c2.Role(3), c2.Term(3))
	fmt.Println("the minority side cannot reach a majority, so no split brain")

	fmt.Println("\n=== why randomised timeouts matter ===")
	splits := 0
	for seed := int64(0); seed < 50; seed++ {
		cc := NewCluster(5, seed)
		if _, ok := cc.RunUntilLeader(500); !ok {
			splits++
		}
	}
	fmt.Printf("50 clusters with randomised timeouts: %d failed to elect\n", splits)
	fmt.Println("with identical timeouts every node would campaign in lockstep,")
	fmt.Println("split the vote every round, and never converge")
}
