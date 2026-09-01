package main

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMachineIDValidation(t *testing.T) {
	for _, id := range []int64{-1, 1024, 99999} {
		if _, err := NewGenerator(id); err == nil {
			t.Errorf("machine ID %d should be rejected", id)
		}
	}
	for _, id := range []int64{0, 512, 1023} {
		if _, err := NewGenerator(id); err != nil {
			t.Errorf("machine ID %d rejected: %v", id, err)
		}
	}
}

// Sortability is why this beats UUIDv4 as a clustered primary key.
func TestIDsAreMonotonic(t *testing.T) {
	g, _ := NewGenerator(1)
	prev := int64(-1)
	for i := 0; i < 50000; i++ {
		id, err := g.Next()
		if err != nil {
			t.Fatal(err)
		}
		if id <= prev {
			t.Fatalf("ID %d did not exceed the previous %d", id, prev)
		}
		prev = id
	}
}

func TestIDsAreUnique(t *testing.T) {
	g, _ := NewGenerator(1)
	seen := make(map[int64]bool, 50000)
	for i := 0; i < 50000; i++ {
		id, _ := g.Next()
		if seen[id] {
			t.Fatalf("duplicate ID %d", id)
		}
		seen[id] = true
	}
}

func TestParseRoundTrip(t *testing.T) {
	g, _ := NewGenerator(42)
	before := time.Now().UnixMilli()
	id, _ := g.Next()
	after := time.Now().UnixMilli()

	ts, machine, seq := Parse(id)
	if machine != 42 {
		t.Errorf("machine = %d, want 42", machine)
	}
	if seq < 0 || seq > maxSequence {
		t.Errorf("sequence = %d, out of range", seq)
	}
	if ms := ts.UnixMilli(); ms < before || ms > after {
		t.Errorf("timestamp %d outside [%d,%d]", ms, before, after)
	}
}

func TestDifferentMachinesNeverCollide(t *testing.T) {
	const machines = 20
	const perMachine = 2000

	var wg sync.WaitGroup
	results := make([][]int64, machines)
	for m := 0; m < machines; m++ {
		wg.Add(1)
		go func(m int) {
			defer wg.Done()
			g, _ := NewGenerator(int64(m))
			ids := make([]int64, perMachine)
			for i := range ids {
				ids[i], _ = g.Next()
			}
			results[m] = ids
		}(m)
	}
	wg.Wait()

	seen := make(map[int64]bool, machines*perMachine)
	for _, ids := range results {
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("collision across machines on ID %d", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != machines*perMachine {
		t.Errorf("got %d unique IDs, want %d", len(seen), machines*perMachine)
	}
}

func TestConcurrentGenerationIsSafe(t *testing.T) {
	g, _ := NewGenerator(1)
	const goroutines = 50
	const each = 500

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[int64]bool, goroutines*each)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				id, err := g.Next()
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				if seen[id] {
					t.Errorf("duplicate ID %d under concurrency", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != goroutines*each {
		t.Errorf("got %d unique IDs, want %d", len(seen), goroutines*each)
	}
}

// A large backward step must fail loudly rather than emit duplicates.
func TestLargeClockBackwardsIsRejected(t *testing.T) {
	g, _ := NewGenerator(1)
	fixed := time.Now().UnixMilli()
	g.now = func() int64 { return fixed }

	if _, err := g.Next(); err != nil {
		t.Fatal(err)
	}
	g.now = func() int64 { return fixed - 10000 } // 10s backwards

	_, err := g.Next()
	if !errors.Is(err, ErrClockBackwards) {
		t.Errorf("err = %v, want ErrClockBackwards", err)
	}
}

// Small NTP corrections are routine and should be waited out, not fatal.
func TestSmallClockBackwardsIsToleratedByWaiting(t *testing.T) {
	g, _ := NewGenerator(1)
	base := time.Now().UnixMilli()
	calls := 0
	g.now = func() int64 {
		calls++
		if calls == 1 {
			return base
		}
		if calls < 5 {
			return base - 2 // small backward step
		}
		return base + 1 // recovered
	}

	if _, err := g.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Next(); err != nil {
		t.Errorf("small drift should be waited out, got %v", err)
	}
}

// Exhausting the sequence must block, never borrow bits from the timestamp.
func TestSequenceExhaustionBlocksForNextMilli(t *testing.T) {
	g, _ := NewGenerator(1)
	milli := time.Now().UnixMilli()
	advance := false
	g.now = func() int64 {
		if advance {
			return milli + 1
		}
		return milli
	}

	// Fill the 4096 slots available in this millisecond.
	for i := 0; i <= maxSequence; i++ {
		if _, err := g.Next(); err != nil {
			t.Fatalf("ID %d: %v", i, err)
		}
	}

	advance = true
	id, err := g.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ts, _, seq := Parse(id); seq != 0 || ts.UnixMilli() != milli+1 {
		t.Errorf("seq = %d at ms %d, want sequence 0 in the next millisecond", seq, ts.UnixMilli())
	}
}

func TestAllIDsAreNonNegative(t *testing.T) {
	g, _ := NewGenerator(1023)
	for i := 0; i < 5000; i++ {
		id, _ := g.Next()
		if id < 0 {
			t.Fatalf("negative ID %d — the sign bit must stay clear", id)
		}
	}
}

func TestBitLayoutConstants(t *testing.T) {
	if timestampBits+machineBits+sequenceBits != 63 {
		t.Error("bit allocation must total 63, leaving the sign bit unused")
	}
	if maxMachineID != 1023 {
		t.Errorf("maxMachineID = %d, want 1023", maxMachineID)
	}
	if maxSequence != 4095 {
		t.Errorf("maxSequence = %d, want 4095", maxSequence)
	}
}

func TestMachineIDIsEncodedCorrectly(t *testing.T) {
	for _, want := range []int64{0, 1, 511, 1023} {
		g, _ := NewGenerator(want)
		id, _ := g.Next()
		if _, got, _ := Parse(id); got != want {
			t.Errorf("machine = %d, want %d", got, want)
		}
	}
}

func BenchmarkNext(b *testing.B) {
	g, _ := NewGenerator(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Next()
	}
}

func BenchmarkNextParallel(b *testing.B) {
	g, _ := NewGenerator(1)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Next()
		}
	})
}
