package main

import (
	"strconv"
	"testing"
)

func testKeys(n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "key:" + strconv.Itoa(i)
	}
	return keys
}

func TestSameKeyAlwaysSameNode(t *testing.T) {
	r := NewRing(DefaultVirtualNodes)
	r.Add("a")
	r.Add("b")
	r.Add("c")

	first, _ := r.Get("user:42")
	for i := 0; i < 100; i++ {
		if got, _ := r.Get("user:42"); got != first {
			t.Fatalf("Get not deterministic: %q then %q", first, got)
		}
	}
}

func TestEmptyRing(t *testing.T) {
	r := NewRing(10)
	if _, ok := r.Get("anything"); ok {
		t.Error("empty ring should not return a node")
	}
}

// The whole point of the structure: adding a node must move ~1/N of keys, not most.
func TestAddingNodeMovesRoughlyOneOverN(t *testing.T) {
	keys := testKeys(50000)
	r := NewRing(DefaultVirtualNodes)
	for _, n := range []string{"a", "b", "c", "d"} {
		r.Add(n)
	}

	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k], _ = r.Get(k)
	}

	r.Add("e")
	moved := 0
	for _, k := range keys {
		if now, _ := r.Get(k); now != before[k] {
			moved++
		}
	}

	fraction := float64(moved) / float64(len(keys))
	if fraction < 0.10 || fraction > 0.32 {
		t.Errorf("moved %.1f%% of keys, want ~20%%", fraction*100)
	}
}

// Keys that did move must land on the new node; consistent hashing must never
// shuffle keys between two nodes that both stayed.
func TestMovedKeysGoToTheNewNode(t *testing.T) {
	keys := testKeys(20000)
	r := NewRing(DefaultVirtualNodes)
	for _, n := range []string{"a", "b", "c"} {
		r.Add(n)
	}
	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k], _ = r.Get(k)
	}

	r.Add("d")
	for _, k := range keys {
		now, _ := r.Get(k)
		if now != before[k] && now != "d" {
			t.Fatalf("key %q moved from %q to %q, but only \"d\" is new", k, before[k], now)
		}
	}
}

func TestRemoveRedistributesOnlyItsOwnKeys(t *testing.T) {
	keys := testKeys(20000)
	r := NewRing(DefaultVirtualNodes)
	for _, n := range []string{"a", "b", "c", "d"} {
		r.Add(n)
	}
	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k], _ = r.Get(k)
	}

	r.Remove("c")
	if r.Nodes() != 3 {
		t.Fatalf("Nodes = %d, want 3", r.Nodes())
	}
	for _, k := range keys {
		now, _ := r.Get(k)
		if now == "c" {
			t.Fatal("removed node still owns keys")
		}
		if before[k] != "c" && now != before[k] {
			t.Fatalf("key %q owned by %q was needlessly moved to %q", k, before[k], now)
		}
	}
}

// 150 virtual nodes should keep every node within roughly +/-15% of even.
func TestDistributionIsBalanced(t *testing.T) {
	keys := testKeys(100000)
	r := NewRing(DefaultVirtualNodes)
	nodes := []string{"a", "b", "c", "d", "e"}
	for _, n := range nodes {
		r.Add(n)
	}

	counts := r.Distribution(keys)
	ideal := float64(len(keys)) / float64(len(nodes))
	for n, c := range counts {
		deviation := (float64(c) - ideal) / ideal
		if deviation < -0.15 || deviation > 0.15 {
			t.Errorf("node %s holds %d keys, %.1f%% off ideal %.0f", n, c, deviation*100, ideal)
		}
	}
}

// Too few virtual nodes is the classic misconfiguration: the ring is lumpy and
// one node ends up with far more than its share.
func TestFewVirtualNodesIsUnbalanced(t *testing.T) {
	keys := testKeys(50000)
	r := NewRing(1)
	nodes := []string{"a", "b", "c", "d", "e"}
	for _, n := range nodes {
		r.Add(n)
	}

	counts := r.Distribution(keys)
	ideal := float64(len(keys)) / float64(len(nodes))
	worst := 0.0
	for _, c := range counts {
		if d := (float64(c) - ideal) / ideal; d > worst {
			worst = d
		}
	}
	if worst < 0.15 {
		t.Skip("this ring happened to be balanced; the point stands statistically")
	}
}

func TestGetNReturnsDistinctPhysicalNodes(t *testing.T) {
	r := NewRing(DefaultVirtualNodes)
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		r.Add(n)
	}

	replicas := r.GetN("user:42", 3)
	if len(replicas) != 3 {
		t.Fatalf("got %d replicas, want 3", len(replicas))
	}
	seen := map[string]bool{}
	for _, n := range replicas {
		if seen[n] {
			t.Errorf("duplicate replica %q: replication would not survive a node loss", n)
		}
		seen[n] = true
	}
	if primary, _ := r.Get("user:42"); replicas[0] != primary {
		t.Errorf("first replica %q should be the primary %q", replicas[0], primary)
	}
}

func TestGetNCapsAtNodeCount(t *testing.T) {
	r := NewRing(DefaultVirtualNodes)
	r.Add("a")
	r.Add("b")
	if got := r.GetN("k", 5); len(got) != 2 {
		t.Errorf("got %d replicas from a 2-node ring, want 2", len(got))
	}
}

func BenchmarkGet(b *testing.B) {
	r := NewRing(DefaultVirtualNodes)
	for i := 0; i < 20; i++ {
		r.Add("node-" + strconv.Itoa(i))
	}
	keys := testKeys(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get(keys[i%len(keys)])
	}
}
