package main

import (
	"os"
	"path/filepath"
	"testing"
)

func tempWAL(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.wal")
}

func TestWriteThenRecover(t *testing.T) {
	path := tempWAL(t)

	w, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	w.Put("a", "1")
	w.Put("b", "2")
	w.Put("a", "updated")
	w.Delete("b")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	state, seq, err := Recover(path)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 4 {
		t.Errorf("seq = %d, want 4", seq)
	}
	if state["a"] != "updated" {
		t.Errorf("a = %q, want \"updated\" — later writes must win", state["a"])
	}
	if _, ok := state["b"]; ok {
		t.Error("b should be absent after a delete record")
	}
}

func TestRecoverMissingFile(t *testing.T) {
	state, seq, err := Recover(filepath.Join(t.TempDir(), "nope.wal"))
	if err != nil {
		t.Fatalf("err = %v, want nil for a missing log", err)
	}
	if len(state) != 0 || seq != 0 {
		t.Error("missing log should recover to empty state")
	}
}

func TestSequenceNumbersAreMonotonic(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, false)
	defer w.Close()

	var prev uint64
	for i := 0; i < 100; i++ {
		seq, err := w.Put("k", "v")
		if err != nil {
			t.Fatal(err)
		}
		if seq <= prev {
			t.Fatalf("seq %d did not increase from %d", seq, prev)
		}
		prev = seq
	}
}

// Reopening must continue the sequence, not restart it — otherwise replay order
// after a second crash is ambiguous.
func TestSequenceContinuesAcrossReopen(t *testing.T) {
	path := tempWAL(t)

	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Put("b", "2")
	w.Close()

	w2, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	seq, _ := w2.Put("c", "3")
	if seq != 3 {
		t.Errorf("seq after reopen = %d, want 3", seq)
	}
}

func TestAppendsAcrossReopenAreAllRecovered(t *testing.T) {
	path := tempWAL(t)

	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Close()

	w2, _ := Open(path, true)
	w2.Put("b", "2")
	w2.Close()

	state, _, _ := Recover(path)
	if len(state) != 2 {
		t.Errorf("recovered %d keys, want 2", len(state))
	}
}

// A crash mid-append leaves a partial record. Recovery must keep everything
// before it and silently drop the fragment.
func TestTornWriteIsDiscarded(t *testing.T) {
	path := tempWAL(t)

	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Put("b", "2")
	w.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{0x01, 0x02, 0x03}) // truncated header
	f.Close()

	state, seq, err := Recover(path)
	if err != nil {
		t.Fatalf("err = %v, a torn tail is expected, not an error", err)
	}
	if len(state) != 2 || seq != 2 {
		t.Errorf("recovered %d keys at seq %d, want 2 and 2", len(state), seq)
	}
}

// Bit rot inside a record body must be caught by the CRC, not silently applied.
func TestCorruptRecordIsDetected(t *testing.T) {
	path := tempWAL(t)

	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Put("b", "2")
	w.Put("c", "3")
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a bit in the second record's payload.
	offset := headerSize + 2 + headerSize + 1
	data[offset] ^= 0xff
	os.WriteFile(path, data, 0o644)

	state, _, _ := Recover(path)
	if len(state) != 1 {
		t.Errorf("recovered %d keys, want 1 — replay must stop at the corrupt record", len(state))
	}
	if state["a"] != "1" {
		t.Error("the record before the corruption should survive")
	}
}

// A corrupt length field must not cause a huge allocation.
func TestAbsurdLengthIsRejected(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Close()

	data, _ := os.ReadFile(path)
	data[13], data[14], data[15], data[16] = 0xff, 0xff, 0xff, 0xff
	os.WriteFile(path, data, 0o644)

	if _, _, err := Recover(path); err != nil {
		t.Errorf("err = %v, want a clean stop", err)
	}
}

func TestEmptyLog(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, true)
	w.Close()

	records, err := Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("got %d records from an empty log", len(records))
	}
}

func TestTruncateCheckpoints(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, true)
	w.Put("a", "1")
	w.Put("b", "2")
	if err := w.Truncate(); err != nil {
		t.Fatal(err)
	}
	w.Close()

	state, _, _ := Recover(path)
	if len(state) != 0 {
		t.Errorf("recovered %d keys after truncation, want 0", len(state))
	}
}

func TestBinaryValuesRoundTrip(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, true)

	value := string([]byte{0x00, 0xff, 0x0a, 0x0d, 0x1a})
	w.Put("binary", value)
	w.Put("empty", "")
	w.Close()

	state, _, _ := Recover(path)
	if state["binary"] != value {
		t.Error("binary value did not survive the round trip")
	}
	if v, ok := state["empty"]; !ok || v != "" {
		t.Error("empty value should be preserved and distinct from absent")
	}
}

func TestReplayPreservesOrder(t *testing.T) {
	path := tempWAL(t)
	w, _ := Open(path, true)
	for i := 0; i < 50; i++ {
		w.Put("k", string(rune('a'+i%26)))
	}
	w.Close()

	records, _ := Replay(path)
	if len(records) != 50 {
		t.Fatalf("got %d records, want 50", len(records))
	}
	for i, r := range records {
		if r.Seq != uint64(i+1) {
			t.Fatalf("record %d has seq %d — replay order is wrong", i, r.Seq)
		}
	}
}

// Buffered writes are much faster but are not durable until Sync.
func BenchmarkPutBuffered(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.wal")
	w, _ := Open(path, false)
	defer w.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Put("key", "value")
	}
}

func BenchmarkPutSynced(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.wal")
	w, _ := Open(path, true)
	defer w.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Put("key", "value")
	}
}
