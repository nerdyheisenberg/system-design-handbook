package main

import (
	"testing"
)

func TestMajorityCalculation(t *testing.T) {
	for size, want := range map[int]int{1: 1, 2: 2, 3: 2, 4: 3, 5: 3, 6: 4, 7: 4} {
		if got := NewCluster(size, 1).majority(); got != want {
			t.Errorf("majority(%d) = %d, want %d", size, got, want)
		}
	}
}

// An even cluster size buys nothing: 4 nodes tolerate the same single failure as
// 3, while adding another thing that can break.
func TestEvenSizesTolerateNoExtraFailure(t *testing.T) {
	three := NewCluster(3, 1)
	four := NewCluster(4, 1)

	tolerate3 := 3 - three.majority()
	tolerate4 := 4 - four.majority()
	if tolerate3 != tolerate4 {
		t.Errorf("3 nodes tolerate %d failures, 4 tolerate %d — expected the same", tolerate3, tolerate4)
	}
}

func TestElectsALeader(t *testing.T) {
	c := NewCluster(5, 1)
	leader, ok := c.RunUntilLeader(500)
	if !ok {
		t.Fatal("no leader elected within the tick budget")
	}
	if c.Role(leader) != Leader {
		t.Errorf("node %d role = %s, want leader", leader, c.Role(leader))
	}
}

// The core safety property. Randomised timeouts mean the path to a leader varies
// by seed, but the invariant must hold for every one of them.
func TestNeverTwoLeadersInTheSameTerm(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		c := NewCluster(5, seed)
		for i := 0; i < 300; i++ {
			c.Step()
			for term, leaders := range c.LeadersByTerm() {
				if len(leaders) > 1 {
					t.Fatalf("seed %d: term %d has leaders %v — split brain", seed, term, leaders)
				}
			}
		}
	}
}

func TestElectionTerminatesForAllSeeds(t *testing.T) {
	failures := 0
	for seed := int64(0); seed < 100; seed++ {
		c := NewCluster(5, seed)
		if _, ok := c.RunUntilLeader(1000); !ok {
			failures++
		}
	}
	if failures > 0 {
		t.Errorf("%d/100 clusters failed to elect a leader", failures)
	}
}

func TestTermsOnlyIncrease(t *testing.T) {
	c := NewCluster(5, 3)
	prev := make([]int, 5)

	for i := 0; i < 300; i++ {
		c.Step()
		for id := 0; id < 5; id++ {
			term := c.Term(id)
			if term < prev[id] {
				t.Fatalf("node %d term went backwards: %d then %d", id, prev[id], term)
			}
			prev[id] = term
		}
	}
}

// ⭐ The property that prevents split brain during a partition: a minority side
// cannot reach a majority, so it cannot elect anyone.
func TestMinorityPartitionElectsNoLeader(t *testing.T) {
	c := NewCluster(5, 7)
	if _, ok := c.RunUntilLeader(500); !ok {
		t.Fatal("setup: no initial leader")
	}

	c.Partition(3, 4) // isolate 2 of 5
	for i := 0; i < 500; i++ {
		c.Step()
	}

	for _, id := range []int{3, 4} {
		if c.Role(id) == Leader {
			t.Errorf("node %d in the minority became leader — split brain", id)
		}
	}
}

// The majority side must keep functioning, otherwise the partition costs
// availability that quorums are supposed to preserve.
func TestMajorityPartitionKeepsALeader(t *testing.T) {
	c := NewCluster(5, 11)
	c.RunUntilLeader(500)
	c.Partition(3, 4)

	found := false
	for i := 0; i < 500; i++ {
		c.Step()
		for _, id := range c.Leaders() {
			if id < 3 {
				found = true
			}
		}
	}
	if !found {
		t.Error("the majority side never had a leader")
	}
}

// A node isolated alone campaigns forever without ever winning.
func TestIsolatedNodeCampaignsButNeverWins(t *testing.T) {
	c := NewCluster(5, 13)
	c.RunUntilLeader(300)
	c.Partition(4)

	startTerm := c.Term(4)
	for i := 0; i < 500; i++ {
		c.Step()
		if c.Role(4) == Leader {
			t.Fatal("an isolated node became leader")
		}
	}
	if c.Term(4) <= startTerm {
		t.Error("the isolated node should have kept incrementing its term")
	}
}

func TestClusterRecoversAfterHealing(t *testing.T) {
	c := NewCluster(5, 17)
	c.RunUntilLeader(300)
	c.Partition(3, 4)
	for i := 0; i < 300; i++ {
		c.Step()
	}

	c.Heal()
	for i := 0; i < 500; i++ {
		c.Step()
	}

	if leaders := c.Leaders(); len(leaders) != 1 {
		t.Errorf("after healing, leaders = %v, want exactly 1", leaders)
	}
}

// A node votes at most once per term — the mechanism behind the safety property.
func TestOneVotePerTerm(t *testing.T) {
	c := NewCluster(5, 1)
	voter := c.nodes[0]

	if !c.requestVote(voter, 5, 1) {
		t.Fatal("first vote in term 5 should be granted")
	}
	if c.requestVote(voter, 5, 2) {
		t.Error("a second vote in the same term must be refused")
	}
	// Idempotent: the same candidate asking again is fine.
	if !c.requestVote(voter, 5, 1) {
		t.Error("re-requesting from the same candidate should succeed")
	}
}

func TestStaleCandidateIsRejected(t *testing.T) {
	c := NewCluster(5, 1)
	voter := c.nodes[0]
	c.requestVote(voter, 10, 1)

	if c.requestVote(voter, 5, 2) {
		t.Error("a candidate with a lower term must be rejected")
	}
}

// A higher term demotes anyone, including a leader — this is how a stale leader
// steps down after a partition heals.
func TestHigherTermDemotesLeader(t *testing.T) {
	c := NewCluster(5, 1)
	leader := c.nodes[0]

	leader.mu.Lock()
	leader.role, leader.term = Leader, 5
	leader.mu.Unlock()

	if !c.requestVote(leader, 10, 1) {
		t.Fatal("a higher-term vote request should be granted")
	}
	if c.Role(0) != Follower {
		t.Errorf("role = %s, want follower after seeing a higher term", c.Role(0))
	}
	if c.Term(0) != 10 {
		t.Errorf("term = %d, want 10", c.Term(0))
	}
}

func TestSingleNodeClusterElectsItself(t *testing.T) {
	c := NewCluster(1, 1)
	if _, ok := c.RunUntilLeader(200); !ok {
		t.Error("a single-node cluster should elect itself")
	}
}

func TestLargeClusterElects(t *testing.T) {
	for _, size := range []int{3, 5, 7, 9, 15} {
		c := NewCluster(size, 42)
		if _, ok := c.RunUntilLeader(2000); !ok {
			t.Errorf("cluster of %d failed to elect", size)
		}
	}
}

// Heartbeats are what stop followers from starting pointless elections.
func TestHeartbeatsSuppressElections(t *testing.T) {
	c := NewCluster(5, 23)
	leader, ok := c.RunUntilLeader(300)
	if !ok {
		t.Fatal("no leader")
	}

	termAfterElection := c.Term(leader)
	for i := 0; i < 500; i++ {
		c.Step()
	}

	if got := c.Term(leader); got > termAfterElection+1 {
		t.Errorf("term climbed from %d to %d with a healthy leader — heartbeats are not suppressing elections",
			termAfterElection, got)
	}
}

func TestRoleString(t *testing.T) {
	for r, want := range map[Role]string{
		Follower: "follower", Candidate: "candidate", Leader: "leader",
	} {
		if r.String() != want {
			t.Errorf("String() = %q, want %q", r.String(), want)
		}
	}
}
