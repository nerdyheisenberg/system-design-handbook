// Package main implements a write-ahead log with crash recovery. See Chapter 6.
//
// The rule: log the intent before applying the change. After a crash, replay the
// log to reconstruct any state that was acknowledged but not yet persisted in
// place. This is how every relational database gets durability without paying for
// a random write to the data file on every commit.
//
// Record layout, all little-endian:
//
//	 0..3   CRC32 of everything after this field
//	 4..11  sequence number (uint64)
//	12      operation (1 = put, 2 = delete)
//	13..16  key length (uint32)
//	17..20  value length (uint32)
//	21..    key bytes, then value bytes
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const headerSize = 21

type OpType uint8

const (
	OpPut    OpType = 1
	OpDelete OpType = 2
)

type Record struct {
	Seq   uint64
	Op    OpType
	Key   string
	Value string
}

// ErrCorrupt marks a record whose checksum failed. Recovery treats this as the
// end of the valid log rather than an error: a torn final write is the expected
// result of a crash mid-append.
var ErrCorrupt = errors.New("wal: checksum mismatch")

type WAL struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	seq  uint64
	path string
	// syncOnWrite trades throughput for durability. With it off, an OS crash can
	// lose recent records even though Write returned.
	syncOnWrite bool
}

func Open(path string, syncOnWrite bool) (*WAL, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &WAL{f: f, w: bufio.NewWriter(f), path: path, syncOnWrite: syncOnWrite}

	// Continue the sequence from whatever survived the last run.
	records, err := readAll(path)
	if err != nil {
		f.Close()
		return nil, err
	}
	if n := len(records); n > 0 {
		w.seq = records[n-1].Seq
	}
	return w, nil
}

func (w *WAL) Put(key, value string) (uint64, error) {
	return w.append(OpPut, key, value)
}

func (w *WAL) Delete(key string) (uint64, error) {
	return w.append(OpDelete, key, "")
}

func (w *WAL) append(op OpType, key, value string) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seq++
	buf := make([]byte, headerSize+len(key)+len(value))
	binary.LittleEndian.PutUint64(buf[4:], w.seq)
	buf[12] = byte(op)
	binary.LittleEndian.PutUint32(buf[13:], uint32(len(key)))
	binary.LittleEndian.PutUint32(buf[17:], uint32(len(value)))
	copy(buf[headerSize:], key)
	copy(buf[headerSize+len(key):], value)
	binary.LittleEndian.PutUint32(buf[0:], crc32.ChecksumIEEE(buf[4:]))

	if _, err := w.w.Write(buf); err != nil {
		return 0, err
	}
	if w.syncOnWrite {
		if err := w.flushAndSyncLocked(); err != nil {
			return 0, err
		}
	}
	return w.seq, nil
}

// Sync flushes the userspace buffer and asks the OS to persist to the device.
// Group-committing several records before calling this is the standard way to
// amortise the fsync cost.
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushAndSyncLocked()
}

func (w *WAL) flushAndSyncLocked() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.f.Sync()
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.flushAndSyncLocked(); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

func (w *WAL) Seq() uint64 { return w.seq }

// Truncate discards the log after its contents have been durably applied
// elsewhere. This is checkpointing: without it the log grows forever and
// recovery time grows with it.
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.w.Flush(); err != nil {
		return err
	}
	if err := w.f.Truncate(0); err != nil {
		return err
	}
	_, err := w.f.Seek(0, io.SeekStart)
	return err
}

// Replay reads every valid record. It stops at the first corrupt or truncated
// record, which is the correct behaviour after a crash mid-append.
func Replay(path string) ([]Record, error) { return readAll(path) }

func readAll(path string) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var out []Record
	header := make([]byte, headerSize)

	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return out, nil // clean end, or a torn header
			}
			return out, err
		}

		want := binary.LittleEndian.Uint32(header[0:])
		seq := binary.LittleEndian.Uint64(header[4:])
		op := OpType(header[12])
		keyLen := binary.LittleEndian.Uint32(header[13:])
		valLen := binary.LittleEndian.Uint32(header[17:])

		// A corrupt length field could otherwise request an enormous allocation.
		if keyLen > 1<<24 || valLen > 1<<24 {
			return out, nil
		}

		body := make([]byte, keyLen+valLen)
		if _, err := io.ReadFull(r, body); err != nil {
			return out, nil // torn body
		}

		crc := crc32.NewIEEE()
		crc.Write(header[4:])
		crc.Write(body)
		if crc.Sum32() != want {
			return out, nil // corrupt: treat as end of log
		}

		out = append(out, Record{
			Seq:   seq,
			Op:    op,
			Key:   string(body[:keyLen]),
			Value: string(body[keyLen:]),
		})
	}
}

// Recover rebuilds the key-value state implied by the log.
func Recover(path string) (map[string]string, uint64, error) {
	records, err := Replay(path)
	if err != nil {
		return nil, 0, err
	}
	state := make(map[string]string)
	var seq uint64
	for _, r := range records {
		seq = r.Seq
		switch r.Op {
		case OpPut:
			state[r.Key] = r.Value
		case OpDelete:
			delete(state, r.Key)
		}
	}
	return state, seq, nil
}

func main() {
	path := filepath.Join(os.TempDir(), "systemdesign-demo.wal")
	os.Remove(path)

	w, err := Open(path, true)
	if err != nil {
		panic(err)
	}
	w.Put("user:1", "alice")
	w.Put("user:2", "bob")
	w.Put("user:1", "alice-updated")
	w.Delete("user:2")
	w.Put("user:3", "carol")
	w.Close()

	fmt.Println("simulating a crash — reopening and replaying the log")
	state, seq, err := Recover(path)
	if err != nil {
		panic(err)
	}
	fmt.Printf("recovered to sequence %d:\n", seq)
	for _, k := range []string{"user:1", "user:2", "user:3"} {
		if v, ok := state[k]; ok {
			fmt.Printf("  %s = %s\n", k, v)
		} else {
			fmt.Printf("  %s = <deleted>\n", k)
		}
	}

	// A torn final write is normal after a crash; recovery must tolerate it.
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	f.Write([]byte{0xde, 0xad, 0xbe, 0xef, 0x00})
	f.Close()

	state2, seq2, _ := Recover(path)
	fmt.Printf("\nafter appending garbage: recovered %d keys up to sequence %d\n", len(state2), seq2)
	fmt.Println("the partial record is discarded, committed data is intact")

	os.Remove(path)
}
