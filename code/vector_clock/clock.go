// Package main implements vector clocks and conflict detection. See Chapter 21.
//
// The problem physical timestamps cannot solve: two replicas write concurrently,
// their clocks disagree by tens of milliseconds, and last-write-wins silently
// discards one of them. You cannot tell "B happened after A" from "B and A
// happened at the same time on different nodes" using wall-clock time.
//
// A vector clock records one counter per node. Comparing two vectors gives one of
// four answers — equal, before, after, or CONCURRENT — and that fourth answer is
// the one physical timestamps cannot produce. Detecting concurrency is what lets
// a system surface a genuine conflict instead of losing data.
//
// The cost: the vector grows with the number of writers, and the application has
// to decide what to do with siblings. Amazon's own Dynamo retrospective noted
// developers found that hard, which is why many systems default to LWW anyway —
// but they should do so knowingly.
package main

import (
	"fmt"
	"sort"
	"strings"
)

type Ordering int

const (
	Equal Ordering = iota
	Before
	After
	Concurrent
)

func (o Ordering) String() string {
	switch o {
	case Equal:
		return "equal"
	case Before:
		return "before"
	case After:
		return "after"
	case Concurrent:
		return "concurrent"
	}
	return "unknown"
}

// VectorClock maps a node identifier to that node's event counter.
type VectorClock map[string]uint64

func NewVectorClock() VectorClock { return VectorClock{} }

// Increment records a local event on the given node.
func (vc VectorClock) Increment(node string) VectorClock {
	out := vc.Copy()
	out[node]++
	return out
}

func (vc VectorClock) Copy() VectorClock {
	out := make(VectorClock, len(vc))
	for k, v := range vc {
		out[k] = v
	}
	return out
}

// Merge takes the element-wise maximum, which is what a node does when it
// receives a message: it now knows everything both clocks knew.
func (vc VectorClock) Merge(other VectorClock) VectorClock {
	out := vc.Copy()
	for node, count := range other {
		if count > out[node] {
			out[node] = count
		}
	}
	return out
}

// Compare returns how vc relates to other in the happens-before partial order.
func (vc VectorClock) Compare(other VectorClock) Ordering {
	var less, greater bool

	nodes := map[string]bool{}
	for n := range vc {
		nodes[n] = true
	}
	for n := range other {
		nodes[n] = true
	}

	for n := range nodes {
		a, b := vc[n], other[n] // missing entries are implicitly 0
		if a < b {
			less = true
		}
		if a > b {
			greater = true
		}
	}

	switch {
	case !less && !greater:
		return Equal
	case less && !greater:
		return Before
	case greater && !less:
		return After
	default:
		// Each has seen something the other has not: a genuine conflict.
		return Concurrent
	}
}

// HappensBefore reports the strict causal ordering.
func (vc VectorClock) HappensBefore(other VectorClock) bool {
	return vc.Compare(other) == Before
}

func (vc VectorClock) ConcurrentWith(other VectorClock) bool {
	return vc.Compare(other) == Concurrent
}

func (vc VectorClock) String() string {
	if len(vc) == 0 {
		return "{}"
	}
	nodes := make([]string, 0, len(vc))
	for n := range vc {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = fmt.Sprintf("%s:%d", n, vc[n])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Version is a value tagged with the clock at which it was written.
type Version struct {
	Value string
	Clock VectorClock
}

// Replica is a node holding possibly-conflicting versions of one key.
type Replica struct {
	ID       string
	Versions []Version
}

func NewReplica(id string) *Replica {
	return &Replica{ID: id}
}

// Write creates a new version that causally succeeds everything currently held,
// so a local write always resolves existing siblings.
func (r *Replica) Write(value string) {
	clock := NewVectorClock()
	for _, v := range r.Versions {
		clock = clock.Merge(v.Clock)
	}
	clock = clock.Increment(r.ID)
	r.Versions = []Version{{Value: value, Clock: clock}}
}

// Receive merges an incoming version, discarding anything it supersedes and
// keeping siblings when the two are concurrent.
func (r *Replica) Receive(incoming Version) {
	var kept []Version
	for _, existing := range r.Versions {
		switch existing.Clock.Compare(incoming.Clock) {
		case After, Equal:
			// What we hold is newer or identical; the incoming version adds nothing.
			return
		case Before:
			continue // superseded, drop it
		case Concurrent:
			kept = append(kept, existing)
		}
	}
	r.Versions = append(kept, incoming)
}

// Siblings reports whether this replica is holding an unresolved conflict.
func (r *Replica) Siblings() bool { return len(r.Versions) > 1 }

func (r *Replica) Values() []string {
	out := make([]string, len(r.Versions))
	for i, v := range r.Versions {
		out[i] = v.Value
	}
	sort.Strings(out)
	return out
}

func main() {
	fmt.Println("=== causal ordering ===")
	a := NewVectorClock().Increment("A") // {A:1}
	b := a.Increment("A")                // {A:2}, causally after
	fmt.Printf("%s vs %s -> %s\n", a, b, a.Compare(b))

	fmt.Println("\n=== concurrent writes ===")
	base := NewVectorClock().Increment("A")
	x := base.Increment("B") // B extends A's history
	y := base.Increment("C") // C extends the same history independently
	fmt.Printf("%s vs %s -> %s\n", x, y, x.Compare(y))
	fmt.Println("neither saw the other, so this is a real conflict —")
	fmt.Println("last-write-wins would silently discard one of them")

	fmt.Println("\n=== replica simulation ===")
	r1, r2 := NewReplica("node1"), NewReplica("node2")

	r1.Write("alice@example.com")
	r2.Receive(r1.Versions[0])
	fmt.Printf("after replication: node2 = %v\n", r2.Values())

	// Partition: both sides write.
	r1.Write("alice@work.com")
	r2.Write("alice@personal.com")
	fmt.Printf("during partition: node1 = %v, node2 = %v\n", r1.Values(), r2.Values())

	// Heal.
	r1.Receive(r2.Versions[0])
	fmt.Printf("after healing: node1 = %v (siblings=%v)\n", r1.Values(), r1.Siblings())
	fmt.Println("the system correctly refuses to guess — the application decides")

	r1.Write("alice@merged.com")
	fmt.Printf("after resolution: node1 = %v (siblings=%v)\n", r1.Values(), r1.Siblings())
}
