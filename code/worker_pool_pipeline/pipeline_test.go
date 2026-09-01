package main

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runtimeGoroutines() int { return runtime.NumGoroutine() }

func identity(ctx context.Context, j Job) (Job, error) { return j, nil }

func TestProcessesEveryJob(t *testing.T) {
	p := NewPipeline(10, 4)
	ctx := context.Background()

	const n = 1000
	out, _ := p.Run(ctx, p.Produce(ctx, n), func(ctx context.Context, j Job) (Job, error) {
		j.Value *= 2
		return j, nil
	})
	results := p.Collect(ctx, out)

	if len(results) != n {
		t.Fatalf("got %d results, want %d", len(results), n)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	for i, r := range results {
		if r.ID != i || r.Value != i*2 {
			t.Fatalf("result %d = %+v, want ID %d value %d", i, r, i, i*2)
		}
	}
}

func TestMultipleStagesCompose(t *testing.T) {
	p := NewPipeline(10, 4)
	ctx := context.Background()

	const n = 500
	s1, _ := p.Run(ctx, p.Produce(ctx, n), func(ctx context.Context, j Job) (Job, error) {
		j.Value += 1
		return j, nil
	})
	s2, _ := p.Run(ctx, s1, func(ctx context.Context, j Job) (Job, error) {
		j.Value *= 10
		return j, nil
	})
	results := p.Collect(ctx, s2)

	if len(results) != n {
		t.Fatalf("got %d results, want %d", len(results), n)
	}
	for _, r := range results {
		if want := (r.ID + 1) * 10; r.Value != want {
			t.Fatalf("job %d = %d, want %d", r.ID, r.Value, want)
		}
	}
}

// ⭐ The core property: a bounded channel means a slow consumer stops a fast
// producer, so memory stays bounded instead of growing without limit.
func TestBackpressureLimitsInFlightWork(t *testing.T) {
	const bufferSize = 3
	const workers = 2

	p := NewPipeline(bufferSize, workers)
	ctx := context.Background()

	release := make(chan struct{})
	var started atomic.Int64

	out, _ := p.Run(ctx, p.Produce(ctx, 1000), func(ctx context.Context, j Job) (Job, error) {
		started.Add(1)
		<-release // never completes until we say so
		return j, nil
	})

	// Give the producer every chance to run ahead.
	time.Sleep(100 * time.Millisecond)

	// At most: workers in the stage + producer buffer + output buffer.
	maxPossible := int64(workers + bufferSize + bufferSize + 1)
	if got := p.Metrics.Produced.Load(); got > maxPossible {
		t.Errorf("producer emitted %d jobs while the consumer was blocked; "+
			"backpressure should have capped it near %d", got, maxPossible)
	}
	if started.Load() > int64(workers) {
		t.Errorf("%d jobs in flight, want at most %d workers", started.Load(), workers)
	}

	close(release)
	p.Collect(ctx, out)
}

func TestBackpressureIsRecorded(t *testing.T) {
	p := NewPipeline(1, 1)
	ctx := context.Background()

	out, _ := p.Run(ctx, p.Produce(ctx, 200), func(ctx context.Context, j Job) (Job, error) {
		time.Sleep(200 * time.Microsecond)
		return j, nil
	})
	p.Collect(ctx, out)

	if p.Metrics.BlockedSends.Load() == 0 {
		t.Error("a slow stage with a 1-slot buffer should have blocked sends")
	}
}

// A larger buffer absorbs bursts, so backpressure engages less often.
func TestLargerBufferReducesBlocking(t *testing.T) {
	run := func(bufferSize int) int64 {
		p := NewPipeline(bufferSize, 2)
		ctx := context.Background()
		out, _ := p.Run(ctx, p.Produce(ctx, 500), func(ctx context.Context, j Job) (Job, error) {
			time.Sleep(100 * time.Microsecond)
			return j, nil
		})
		p.Collect(ctx, out)
		return p.Metrics.BlockedSends.Load()
	}

	small := run(1)
	large := run(500)
	if large > small {
		t.Errorf("buffer 500 blocked %d times vs %d for buffer 1", large, small)
	}
}

func TestWorkerPoolRunsConcurrently(t *testing.T) {
	const workers = 8
	p := NewPipeline(workers*2, workers)
	ctx := context.Background()

	out, _ := p.Run(ctx, p.Produce(ctx, 200), func(ctx context.Context, j Job) (Job, error) {
		time.Sleep(2 * time.Millisecond)
		return j, nil
	})
	p.Collect(ctx, out)

	peak := p.Metrics.MaxInFlight.Load()
	if peak < 2 {
		t.Errorf("peak concurrency = %d, the pool is not parallel", peak)
	}
	if peak > workers {
		t.Errorf("peak concurrency = %d, exceeds the pool size %d", peak, workers)
	}
}

func TestErrorsGoToTheErrorChannelNotTheOutput(t *testing.T) {
	p := NewPipeline(20, 2)
	ctx := context.Background()

	sentinel := errors.New("bad job")
	out, errs := p.Run(ctx, p.Produce(ctx, 100), func(ctx context.Context, j Job) (Job, error) {
		if j.ID%10 == 0 {
			return Job{}, sentinel
		}
		return j, nil
	})

	var wg sync.WaitGroup
	var failed []Result
	wg.Add(1)
	go func() {
		defer wg.Done()
		for r := range errs {
			failed = append(failed, r)
		}
	}()

	results := p.Collect(ctx, out)
	wg.Wait()

	if len(results) != 90 {
		t.Errorf("got %d successes, want 90", len(results))
	}
	if len(failed) != 10 {
		t.Errorf("got %d failures, want 10", len(failed))
	}
	if p.Metrics.Failed.Load() != 10 {
		t.Errorf("Failed metric = %d, want 10", p.Metrics.Failed.Load())
	}
	for _, r := range failed {
		if !errors.Is(r.Err, sentinel) {
			t.Errorf("error = %v, want the sentinel", r.Err)
		}
	}
}

// A failing job must not stop the pipeline.
func TestPipelineSurvivesFailures(t *testing.T) {
	p := NewPipeline(10, 4)
	ctx := context.Background()

	out, errs := p.Run(ctx, p.Produce(ctx, 100), func(ctx context.Context, j Job) (Job, error) {
		if j.ID < 50 {
			return Job{}, errors.New("fail")
		}
		return j, nil
	})
	go func() {
		for range errs {
		}
	}()

	if got := len(p.Collect(ctx, out)); got != 50 {
		t.Errorf("got %d results, want 50 — later jobs should still flow", got)
	}
}

func TestCancellationStopsThePipeline(t *testing.T) {
	p := NewPipeline(5, 2)
	ctx, cancel := context.WithCancel(context.Background())

	out, _ := p.Run(ctx, p.Produce(ctx, 1000000), func(ctx context.Context, j Job) (Job, error) {
		time.Sleep(time.Millisecond)
		return j, nil
	})

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	results := p.Collect(ctx, out)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("took %v to stop after cancellation", elapsed)
	}
	if len(results) >= 1000000 {
		t.Error("cancellation did not stop the pipeline early")
	}
}

// Cancellation must not leave goroutines parked forever on a channel send.
func TestCancellationDoesNotLeakGoroutines(t *testing.T) {
	before := runtimeGoroutines()

	for i := 0; i < 20; i++ {
		p := NewPipeline(2, 3)
		ctx, cancel := context.WithCancel(context.Background())
		out, _ := p.Run(ctx, p.Produce(ctx, 100000), func(ctx context.Context, j Job) (Job, error) {
			time.Sleep(100 * time.Microsecond)
			return j, nil
		})
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()
		p.Collect(ctx, out)
	}

	// Give stragglers a moment to unwind.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtimeGoroutines() <= before+10 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("goroutines grew from %d to %d — likely a leak", before, runtimeGoroutines())
}

func TestZeroBufferIsUnbufferedNotInvalid(t *testing.T) {
	p := NewPipeline(0, 2)
	ctx := context.Background()

	out, _ := p.Run(ctx, p.Produce(ctx, 100), identity)
	if got := len(p.Collect(ctx, out)); got != 100 {
		t.Errorf("got %d results, want 100", got)
	}
}

func TestNegativeParametersAreClamped(t *testing.T) {
	p := NewPipeline(-5, -3)
	if p.BufferSize != 0 {
		t.Errorf("BufferSize = %d, want 0", p.BufferSize)
	}
	if p.Workers != 1 {
		t.Errorf("Workers = %d, want 1", p.Workers)
	}
}

func TestEmptyInput(t *testing.T) {
	p := NewPipeline(10, 4)
	ctx := context.Background()
	out, _ := p.Run(ctx, p.Produce(ctx, 0), identity)
	if got := len(p.Collect(ctx, out)); got != 0 {
		t.Errorf("got %d results, want 0", got)
	}
}

func BenchmarkPipeline(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewPipeline(100, 4)
		out, _ := p.Run(ctx, p.Produce(ctx, 1000), identity)
		p.Collect(ctx, out)
	}
}
