// Package main implements a 64-bit Snowflake ID generator. See Chapter 23.
//
//	 1        41 bits             10 bits      12 bits
//	┌─┬───────────────────────┬───────────┬─────────────┐
//	│0│ ms since custom epoch │ machine   │  sequence   │
//	└─┴───────────────────────┴───────────┴─────────────┘
//
// Why 64 bits rather than a 128-bit UUID: the ID is usually a clustered primary
// key, so it is repeated in every secondary index. Halving it halves that
// overhead. And because the high bits are a timestamp, IDs are roughly sortable,
// so inserts append to the end of the B+tree instead of fragmenting it —
// a random UUIDv4 clustered key costs roughly 47% index bloat.
//
// Three things go wrong in production, and all three are handled here:
//
//  1. Clock skew. If the clock steps backwards you silently emit duplicates.
//     This implementation refuses rather than continuing.
//  2. Machine ID collisions. Two generators sharing an ID produce duplicates.
//     Allocate via etcd/ZooKeeper ephemeral nodes or a StatefulSet ordinal —
//     never a static config file, because those get copied.
//  3. Sequence exhaustion. More than 4096 IDs in one millisecond must block
//     until the next millisecond, never borrow from the timestamp.
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	timestampBits = 41
	machineBits   = 10
	sequenceBits  = 12

	maxMachineID = -1 ^ (-1 << machineBits)  // 1023
	maxSequence  = -1 ^ (-1 << sequenceBits) // 4095

	machineShift   = sequenceBits
	timestampShift = sequenceBits + machineBits
)

// Epoch is a custom start point. Using 2020 rather than 1970 buys ~50 more years
// out of the 41-bit timestamp.
var Epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// ErrClockBackwards is returned when the system clock moves backwards by more
// than the tolerated drift. Returning an error is the whole point: continuing
// would emit duplicate IDs that nothing would detect.
var ErrClockBackwards = errors.New("snowflake: clock moved backwards")

type Generator struct {
	mu        sync.Mutex
	machineID int64
	sequence  int64
	lastMilli int64

	// maxBackwardWait is how long to wait for a small backward step before
	// giving up. NTP corrections of a few milliseconds are normal; a large jump
	// is an operational problem the generator must not paper over.
	maxBackwardWait time.Duration

	now func() int64
}

func NewGenerator(machineID int64) (*Generator, error) {
	if machineID < 0 || machineID > maxMachineID {
		return nil, fmt.Errorf("snowflake: machine ID %d out of range [0,%d]", machineID, maxMachineID)
	}
	return &Generator{
		machineID:       machineID,
		lastMilli:       -1,
		maxBackwardWait: 5 * time.Millisecond,
		now:             func() int64 { return time.Now().UnixMilli() },
	}, nil
}

func (g *Generator) Next() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()

	if now < g.lastMilli {
		drift := time.Duration(g.lastMilli-now) * time.Millisecond
		if drift > g.maxBackwardWait {
			return 0, fmt.Errorf("%w by %v", ErrClockBackwards, drift)
		}
		// Small drift: wait it out rather than failing.
		for now < g.lastMilli {
			time.Sleep(time.Millisecond)
			now = g.now()
		}
	}

	if now == g.lastMilli {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// Exhausted this millisecond. Block; never borrow from the timestamp.
			for now <= g.lastMilli {
				now = g.now()
			}
		}
	} else {
		g.sequence = 0
	}
	g.lastMilli = now

	elapsed := now - Epoch
	if elapsed < 0 {
		return 0, errors.New("snowflake: current time is before the configured epoch")
	}
	if elapsed >= 1<<timestampBits {
		return 0, errors.New("snowflake: timestamp overflow, the epoch is too old")
	}

	return elapsed<<timestampShift | g.machineID<<machineShift | g.sequence, nil
}

// Parse decomposes an ID, which is useful for debugging and for extracting the
// creation time without a database lookup.
func Parse(id int64) (ts time.Time, machineID, sequence int64) {
	sequence = id & maxSequence
	machineID = (id >> machineShift) & maxMachineID
	ts = time.UnixMilli((id >> timestampShift) + Epoch).UTC()
	return
}

func MaxMachineID() int64 { return maxMachineID }
func MaxSequence() int64  { return maxSequence }

func main() {
	g, err := NewGenerator(42)
	if err != nil {
		panic(err)
	}

	fmt.Println("five consecutive IDs:")
	for i := 0; i < 5; i++ {
		id, _ := g.Next()
		ts, machine, seq := Parse(id)
		fmt.Printf("  %d  ts=%s machine=%d seq=%d\n",
			id, ts.Format("15:04:05.000"), machine, seq)
	}

	fmt.Printf("\ncapacity: %d machines x %d IDs/ms = %d IDs/second/machine\n",
		maxMachineID+1, maxSequence+1, (maxSequence+1)*1000)
	fmt.Printf("41-bit timestamp lasts %.0f years from the epoch\n",
		float64(int64(1)<<timestampBits)/1000/60/60/24/365)

	// Monotonicity is what makes these usable as clustered keys.
	prev := int64(0)
	const n = 100000
	start := time.Now()
	for i := 0; i < n; i++ {
		id, err := g.Next()
		if err != nil {
			panic(err)
		}
		if id <= prev {
			panic("IDs are not monotonic")
		}
		prev = id
	}
	elapsed := time.Since(start)
	fmt.Printf("\ngenerated %d monotonic IDs in %v (%.0f/second)\n",
		n, elapsed.Round(time.Millisecond), float64(n)/elapsed.Seconds())

	// A backward clock must fail loudly.
	g2, _ := NewGenerator(1)
	g2.Next()
	g2.lastMilli = time.Now().UnixMilli() + 10000 // simulate a large backward step
	if _, err := g2.Next(); err != nil {
		fmt.Printf("\nclock moved backwards: %v\n", err)
		fmt.Println("refusing is correct — continuing would emit silent duplicates")
	}
}
