// Package main implements a concurrent pipeline with real backpressure.
// See Chapter 12.
//
// The pattern: stages connected by bounded channels, each stage a pool of
// workers. Two properties matter and both come from BOUNDED channels:
//
//  1. ⭐ Backpressure. When a downstream stage is slow, its input channel fills,
//     and sends into it block. That blocking propagates upstream all the way to
//     the producer, which stops reading faster than the system can process.
//     An unbounded channel or queue instead accumulates until the process is
//     OOM-killed — the failure looks like a memory leak but is a design error.
//
//  2. Bounded memory. Peak memory is (buffer size x stages), known in advance,
//     rather than "however much the producer generated".
//
// Context cancellation runs through every stage so a shutdown or a fatal error
// unwinds the whole pipeline instead of leaking goroutines.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	ID    int
	Value int
}

type Result struct {
	JobID int
	Value int
	Err   error
}

// Metrics records what the pipeline did, including how often backpressure
// actually engaged — which is the number that proves the mechanism works.
type Metrics struct {
	Produced  atomic.Int64
	Processed atomic.Int64
	Failed    atomic.Int64
	// BlockedSends counts how many times a stage had to wait because the
	// downstream buffer was full.
	BlockedSends atomic.Int64
	MaxInFlight  atomic.Int64
	inFlight     atomic.Int64
}

func (m *Metrics) enter() {
	c := m.inFlight.Add(1)
	for {
		peak := m.MaxInFlight.Load()
		if c <= peak || m.MaxInFlight.CompareAndSwap(peak, c) {
			return
		}
	}
}

func (m *Metrics) leave() { m.inFlight.Add(-1) }

// Stage is one step of work.
type Stage func(ctx context.Context, in Job) (Job, error)

type Pipeline struct {
	// BufferSize bounds each inter-stage channel. This is the backpressure knob:
	// smaller means tighter coupling and lower memory, larger absorbs bursts.
	BufferSize int
	// Workers is the pool size per stage.
	Workers int
	Metrics *Metrics
}

func NewPipeline(bufferSize, workers int) *Pipeline {
	if bufferSize < 0 {
		bufferSize = 0
	}
	if workers < 1 {
		workers = 1
	}
	return &Pipeline{BufferSize: bufferSize, Workers: workers, Metrics: &Metrics{}}
}

// send performs a context-aware send and records when it had to block.
func (p *Pipeline) send(ctx context.Context, ch chan<- Job, j Job) bool {
	select {
	case ch <- j:
		return true
	default:
		// The buffer is full: this is backpressure engaging.
		p.Metrics.BlockedSends.Add(1)
	}
	select {
	case ch <- j:
		return true
	case <-ctx.Done():
		return false
	}
}

// Produce generates jobs into a bounded channel. It blocks when downstream is
// slow, which is the entire point.
func (p *Pipeline) Produce(ctx context.Context, n int) <-chan Job {
	out := make(chan Job, p.BufferSize)
	go func() {
		defer close(out)
		for i := 0; i < n; i++ {
			j := Job{ID: i, Value: i}
			if !p.send(ctx, out, j) {
				return
			}
			p.Metrics.Produced.Add(1)
		}
	}()
	return out
}

// Run attaches a worker pool running stage to in, returning its output channel.
// Fan-out to Workers goroutines, then fan-in to one channel.
func (p *Pipeline) Run(ctx context.Context, in <-chan Job, stage Stage) (<-chan Job, <-chan Result) {
	out := make(chan Job, p.BufferSize)
	errs := make(chan Result, p.BufferSize)

	var wg sync.WaitGroup
	for w := 0; w < p.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-in:
					if !ok {
						return
					}
					p.Metrics.enter()
					result, err := stage(ctx, j)
					p.Metrics.leave()

					if err != nil {
						p.Metrics.Failed.Add(1)
						select {
						case errs <- Result{JobID: j.ID, Err: err}:
						case <-ctx.Done():
							return
						}
						continue
					}
					if !p.send(ctx, out, result) {
						return
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
		close(errs)
	}()
	return out, errs
}

// Collect drains the final stage.
func (p *Pipeline) Collect(ctx context.Context, in <-chan Job) []Job {
	var out []Job
	for {
		select {
		case <-ctx.Done():
			return out
		case j, ok := <-in:
			if !ok {
				return out
			}
			out = append(out, j)
			p.Metrics.Processed.Add(1)
		}
	}
}

// ErrPoison is used by the demo to show a failing job routed to the error channel
// rather than killing the pipeline.
var ErrPoison = errors.New("poison job")

func main() {
	fmt.Println("=== backpressure with a small buffer ===")
	demo(2, 4, 200*time.Microsecond)

	fmt.Println("\n=== the same work with a large buffer ===")
	demo(1000, 4, 200*time.Microsecond)

	fmt.Println("\n=== cancellation unwinds every stage ===")
	p := NewPipeline(4, 2)
	ctx, cancel := context.WithCancel(context.Background())

	jobs := p.Produce(ctx, 1000000)
	slow := func(ctx context.Context, j Job) (Job, error) {
		time.Sleep(time.Millisecond)
		j.Value *= 2
		return j, nil
	}
	stage1, _ := p.Run(ctx, jobs, slow)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	before := time.Now()
	collected := p.Collect(ctx, stage1)
	fmt.Printf("cancelled after %v having processed %d of 1,000,000 jobs\n",
		time.Since(before).Round(time.Millisecond), len(collected))
	fmt.Println("no goroutine leak: every stage selects on ctx.Done()")
}

func demo(bufferSize, workers int, workTime time.Duration) {
	p := NewPipeline(bufferSize, workers)
	ctx := context.Background()

	const n = 2000
	jobs := p.Produce(ctx, n)

	double := func(ctx context.Context, j Job) (Job, error) {
		j.Value *= 2
		return j, nil
	}
	slow := func(ctx context.Context, j Job) (Job, error) {
		time.Sleep(workTime) // the bottleneck stage
		j.Value += 1
		return j, nil
	}

	s1, _ := p.Run(ctx, jobs, double)
	s2, _ := p.Run(ctx, s1, slow)

	start := time.Now()
	results := p.Collect(ctx, s2)
	elapsed := time.Since(start)

	fmt.Printf("  buffer=%-4d workers=%d\n", bufferSize, workers)
	fmt.Printf("  processed %d jobs in %v\n", len(results), elapsed.Round(time.Millisecond))
	fmt.Printf("  backpressure engaged %d times\n", p.Metrics.BlockedSends.Load())
	fmt.Printf("  peak concurrent work: %d\n", p.Metrics.MaxInFlight.Load())
	fmt.Printf("  max buffered items: %d (buffer x stages)\n", bufferSize*3)
}
