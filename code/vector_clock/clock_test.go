package main

import (
	"reflect"
	"testing"
)

func TestEmptyClocksAreEqual(t *testing.T) {
	if got := NewVectorClock().Compare(NewVectorClock()); got != Equal {
		t.Errorf("Compare = %s, want equal", got)
	}
}

func TestIncrementCreatesCausalSuccessor(t *testing.T) {
	a := NewVectorClock().Increment("A")
	b := a.Increment("A")

	if got := a.Compare(b); got != Before {
		t.Errorf("Compare = %s, want before", got)
	}
	if got := b.Compare(a); got != After {
		t.Errorf("reverse Compare = %s, want after", got)
	}
	if !a.HappensBefore(b) {
		t.Error("HappensBefore should be true")
	}
}

// Increment must not mutate the receiver, or callers holding an old version see
// it change underneath them.
func TestIncrementDoesNotMutate(t *testing.T) {
	a := NewVectorClock().Increment("A")
	before := a.String()
	a.Increment("A")

	if a.String() != before {
		t.Errorf("receiver mutated: %s became %s", before, a.String())
	}
}

// The case physical timestamps cannot express.
func TestConcurrentWritesAreDetected(t *testing.T) {
	base := NewVectorClock().Increment("A")
	x := base.Increment("B")
	y := base.Increment("C")

	if got := x.Compare(y); got != Concurrent {
		t.Errorf("Compare = %s, want concurrent", got)
	}
	if !x.ConcurrentWith(y) || !y.ConcurrentWith(x) {
		t.Error("concurrency must be symmetric")
	}
	if x.HappensBefore(y) || y.HappensBefore(x) {
		t.Error("neither concurrent clock may happen-before the other")
	}
}

func TestDisjointNodesAreConcurrent(t *testing.T) {
	a := NewVectorClock().Increment("A")
	b := NewVectorClock().Increment("B")
	if got := a.Compare(b); got != Concurrent {
		t.Errorf("Compare = %s, want concurrent", got)
	}
}

// An empty clock precedes anything: it has seen nothing.
func TestEmptyPrecedesNonEmpty(t *testing.T) {
	if got := NewVectorClock().Compare(NewVectorClock().Increment("A")); got != Before {
		t.Errorf("Compare = %s, want before", got)
	}
}

func TestMergeTakesElementwiseMax(t *testing.T) {
	a := VectorClock{"A": 3, "B": 1}
	b := VectorClock{"A": 1, "B": 5, "C": 2}

	got := a.Merge(b)
	want := VectorClock{"A": 3, "B": 5, "C": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merge = %v, want %v", got, want)
	}
}

// A merge must dominate both inputs — that is what makes it a valid join.
func TestMergeDominatesBothInputs(t *testing.T) {
	a := VectorClock{"A": 3, "B": 1}
	b := VectorClock{"A": 1, "B": 5}
	m := a.Merge(b)

	if m.Compare(a) != After {
		t.Error("merge should be after its first input")
	}
	if m.Compare(b) != After {
		t.Error("merge should be after its second input")
	}
}

func TestMergeIsCommutative(t *testing.T) {
	a := VectorClock{"A": 3, "B": 1, "C": 7}
	b := VectorClock{"A": 1, "B": 5, "D": 2}

	if !reflect.DeepEqual(a.Merge(b), b.Merge(a)) {
		t.Error("merge must be commutative")
	}
}

func TestMergeDoesNotMutate(t *testing.T) {
	a := VectorClock{"A": 1}
	b := VectorClock{"B": 1}
	a.Merge(b)

	if len(a) != 1 || len(b) != 1 {
		t.Error("Merge mutated an input")
	}
}

// Causality must survive an arbitrarily long message chain.
func TestTransitiveCausality(t *testing.T) {
	c1 := NewVectorClock().Increment("A")
	c2 := c1.Merge(NewVectorClock()).Increment("B")
	c3 := c2.Merge(NewVectorClock()).Increment("C")

	if !c1.HappensBefore(c2) || !c2.HappensBefore(c3) {
		t.Fatal("direct causality broken")
	}
	if !c1.HappensBefore(c3) {
		t.Error("causality is not transitive")
	}
}

func TestReplicationConverges(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")

	r1.Write("v1")
	r2.Receive(r1.Versions[0])

	if !reflect.DeepEqual(r2.Values(), []string{"v1"}) {
		t.Errorf("r2 = %v, want [v1]", r2.Values())
	}
	if r2.Siblings() {
		t.Error("a simple replication should not create siblings")
	}
}

// A newer version must replace an older one outright, not accumulate.
func TestNewerVersionSupersedes(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")

	r1.Write("v1")
	r2.Receive(r1.Versions[0])
	r1.Write("v2") // causally after v1
	r2.Receive(r1.Versions[0])

	if got := r2.Values(); !reflect.DeepEqual(got, []string{"v2"}) {
		t.Errorf("r2 = %v, want [v2] — the older version should be dropped", got)
	}
}

func TestOlderVersionIsIgnored(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")

	r1.Write("v1")
	old := r1.Versions[0]
	r1.Write("v2")
	r2.Receive(r1.Versions[0])
	r2.Receive(old) // a late-arriving stale write

	if got := r2.Values(); !reflect.DeepEqual(got, []string{"v2"}) {
		t.Errorf("r2 = %v, want [v2] — a stale write must not resurrect", got)
	}
}

// The scenario the whole mechanism exists for: a partition, concurrent writes,
// then a heal that must surface both rather than silently picking one.
func TestPartitionProducesSiblings(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")

	r1.Write("shared")
	r2.Receive(r1.Versions[0])

	r1.Write("from-node1")
	r2.Write("from-node2")

	r1.Receive(r2.Versions[0])

	if !r1.Siblings() {
		t.Fatal("concurrent writes must produce siblings, not silent data loss")
	}
	want := []string{"from-node1", "from-node2"}
	if got := r1.Values(); !reflect.DeepEqual(got, want) {
		t.Errorf("values = %v, want %v", got, want)
	}
}

// A local write is causally after every sibling, so it resolves the conflict.
func TestWriteResolvesSiblings(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")
	r1.Write("shared")
	r2.Receive(r1.Versions[0])
	r1.Write("a")
	r2.Write("b")
	r1.Receive(r2.Versions[0])

	if !r1.Siblings() {
		t.Fatal("setup failed: expected siblings")
	}
	r1.Write("resolved")

	if r1.Siblings() {
		t.Error("a resolving write should collapse siblings")
	}
	if got := r1.Values(); !reflect.DeepEqual(got, []string{"resolved"}) {
		t.Errorf("values = %v, want [resolved]", got)
	}
}

func TestReceiveIsIdempotent(t *testing.T) {
	r1, r2 := NewReplica("n1"), NewReplica("n2")
	r1.Write("v1")

	for i := 0; i < 10; i++ {
		r2.Receive(r1.Versions[0])
	}
	if len(r2.Versions) != 1 {
		t.Errorf("%d versions after 10 identical deliveries, want 1", len(r2.Versions))
	}
}

func TestThreeWayConcurrency(t *testing.T) {
	r1, r2, r3 := NewReplica("n1"), NewReplica("n2"), NewReplica("n3")
	r1.Write("base")
	r2.Receive(r1.Versions[0])
	r3.Receive(r1.Versions[0])

	r1.Write("a")
	r2.Write("b")
	r3.Write("c")

	r1.Receive(r2.Versions[0])
	r1.Receive(r3.Versions[0])

	if got := r1.Values(); len(got) != 3 {
		t.Errorf("values = %v, want 3 siblings", got)
	}
}

func TestOrderingString(t *testing.T) {
	for o, want := range map[Ordering]string{
		Equal: "equal", Before: "before", After: "after", Concurrent: "concurrent",
	} {
		if o.String() != want {
			t.Errorf("String() = %q, want %q", o.String(), want)
		}
	}
}

func TestClockString(t *testing.T) {
	if got := NewVectorClock().String(); got != "{}" {
		t.Errorf("empty String = %q, want {}", got)
	}
	// Sorted, so output is stable across runs.
	if got := (VectorClock{"B": 2, "A": 1}).String(); got != "{A:1, B:2}" {
		t.Errorf("String = %q, want {A:1, B:2}", got)
	}
}
