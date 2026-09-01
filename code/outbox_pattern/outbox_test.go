package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestOrderAndEventCommitTogether(t *testing.T) {
	db := NewDB()
	if err := PlaceOrder(db, "order-1", 100); err != nil {
		t.Fatal(err)
	}

	if _, ok := db.GetOrder("order-1"); !ok {
		t.Error("order was not persisted")
	}
	if got := len(db.UnpublishedEvents(0)); got != 1 {
		t.Errorf("outbox has %d events, want 1", got)
	}
}

// ⭐ The property that makes the pattern work: a failed transaction leaves
// neither the order nor the event, so they can never disagree.
func TestFailedCommitLeavesNeither(t *testing.T) {
	db := NewDB()
	db.failCommit = true

	if err := PlaceOrder(db, "order-1", 100); !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("err = %v, want ErrCommitFailed", err)
	}
	if db.OrderCount() != 0 {
		t.Error("order was persisted despite a failed commit")
	}
	if db.OutboxSize() != 0 {
		t.Error("event was written despite a failed commit — this is the phantom event bug")
	}
}

func TestRollbackDiscardsBoth(t *testing.T) {
	db := NewDB()
	tx := db.Begin()
	tx.InsertOrder(Order{ID: "x", Amount: 1})
	tx.EnqueueEvent("x", "OrderCreated", "{}")
	tx.Rollback()

	if db.OrderCount() != 0 || db.OutboxSize() != 0 {
		t.Error("rollback left state behind")
	}
}

func TestRelayPublishesAndMarks(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}

	for i := 0; i < 5; i++ {
		PlaceOrder(db, fmt.Sprintf("order-%d", i), 100)
	}

	n, err := relay.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("published %d, want 5", n)
	}
	if broker.Count() != 5 {
		t.Errorf("broker received %d, want 5", broker.Count())
	}
	if got := len(db.UnpublishedEvents(0)); got != 0 {
		t.Errorf("%d events still unpublished", got)
	}
}

// ⭐ The scenario the pattern exists for: the broker is down, but the business
// data is safe and the event is not lost.
func TestBrokerFailureLosesNothing(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{FailNext: 100}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}

	PlaceOrder(db, "order-1", 100)
	if _, err := relay.RunOnce(context.Background()); err == nil {
		t.Fatal("expected a publish error")
	}

	if _, ok := db.GetOrder("order-1"); !ok {
		t.Error("order should be persisted regardless of the broker")
	}
	if got := len(db.UnpublishedEvents(0)); got != 1 {
		t.Errorf("%d pending events, want 1 — the event must survive for retry", got)
	}
	if broker.Count() != 0 {
		t.Error("broker should have received nothing")
	}
}

func TestRelayRecoversAfterBrokerReturns(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{FailNext: 3}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}

	for i := 0; i < 5; i++ {
		PlaceOrder(db, fmt.Sprintf("order-%d", i), 100)
	}

	// Three failed attempts, each stopping the batch.
	for i := 0; i < 3; i++ {
		relay.RunOnce(context.Background())
	}
	if broker.Count() != 0 {
		t.Fatalf("broker received %d during the outage", broker.Count())
	}

	if _, err := relay.RunUntilDrained(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if broker.Count() != 5 {
		t.Errorf("broker received %d after recovery, want 5", broker.Count())
	}
	if got := len(db.UnpublishedEvents(0)); got != 0 {
		t.Errorf("%d events still pending", got)
	}
}

func TestFailureAttemptsAreRecorded(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{FailNext: 3, FailErr: errors.New("connection refused")}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}
	PlaceOrder(db, "order-1", 100)

	for i := 0; i < 3; i++ {
		relay.RunOnce(context.Background())
	}

	pending := db.UnpublishedEvents(0)
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1", len(pending))
	}
	if pending[0].Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", pending[0].Attempts)
	}
	if pending[0].LastError != "connection refused" {
		t.Errorf("LastError = %q", pending[0].LastError)
	}
}

// Ordering matters for per-aggregate consistency: a later event must not
// overtake an earlier one.
func TestEventsPublishInOrder(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 100}

	for i := 0; i < 10; i++ {
		tx := db.Begin()
		tx.EnqueueEvent("agg-1", fmt.Sprintf("Event%d", i), "{}")
		tx.Commit()
	}
	relay.RunUntilDrained(context.Background(), 10)

	want := make([]string, 10)
	for i := range want {
		want[i] = fmt.Sprintf("Event%d", i)
	}
	if got := broker.Types(); !reflect.DeepEqual(got, want) {
		t.Errorf("published order = %v, want %v", got, want)
	}
}

// A mid-batch failure must not let subsequent events jump ahead.
func TestFailureStopsTheBatchToPreserveOrder(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 100}

	for i := 0; i < 5; i++ {
		tx := db.Begin()
		tx.EnqueueEvent("agg-1", fmt.Sprintf("Event%d", i), "{}")
		tx.Commit()
	}

	// Publish the first two, then start failing.
	relay.Batch = 2
	relay.RunOnce(context.Background())
	broker.FailNext = 1
	relay.Batch = 100
	relay.RunOnce(context.Background())

	if got := broker.Types(); !reflect.DeepEqual(got, []string{"Event0", "Event1"}) {
		t.Errorf("published %v, want only the first two", got)
	}
	if got := len(db.UnpublishedEvents(0)); got != 3 {
		t.Errorf("%d pending, want 3", got)
	}
}

func TestBatchSizeIsRespected(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 3}

	for i := 0; i < 10; i++ {
		PlaceOrder(db, fmt.Sprintf("order-%d", i), 100)
	}

	n, _ := relay.RunOnce(context.Background())
	if n != 3 {
		t.Errorf("published %d, want the batch size 3", n)
	}
	if got := len(db.UnpublishedEvents(0)); got != 7 {
		t.Errorf("%d pending, want 7", got)
	}
}

// At-least-once is the accepted cost: republishing an already-sent event is
// possible if the relay crashes between publish and mark.
func TestRepublishIsPossibleAfterACrash(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}
	PlaceOrder(db, "order-1", 100)

	events := db.UnpublishedEvents(0)
	// Simulate: published successfully, then crashed before MarkPublished.
	broker.Publish(context.Background(), events[0])

	relay.RunOnce(context.Background())

	if broker.Count() != 2 {
		t.Errorf("broker received %d copies, want 2 — this documents at-least-once", broker.Count())
	}
	if got := len(db.UnpublishedEvents(0)); got != 0 {
		t.Errorf("%d still pending", got)
	}
}

func TestEmptyOutboxIsANoop(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}

	n, err := relay.RunOnce(context.Background())
	if err != nil || n != 0 {
		t.Errorf("RunOnce = %d, %v; want 0, nil", n, err)
	}
}

func TestContextCancellationStopsTheRelay(t *testing.T) {
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 100}
	for i := 0; i < 100; i++ {
		PlaceOrder(db, fmt.Sprintf("order-%d", i), 100)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := relay.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if broker.Count() != 0 {
		t.Error("a cancelled relay should publish nothing")
	}
}

func TestDoubleCommitIsRejected(t *testing.T) {
	db := NewDB()
	tx := db.Begin()
	tx.InsertOrder(Order{ID: "x"})
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Error("committing twice should fail")
	}
	if db.OrderCount() != 1 {
		t.Errorf("OrderCount = %d, want 1", db.OrderCount())
	}
}

func TestConcurrentWritersAreSafe(t *testing.T) {
	db := NewDB()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			PlaceOrder(db, fmt.Sprintf("order-%d", i), int64(i))
		}(i)
	}
	wg.Wait()

	if db.OrderCount() != 100 {
		t.Errorf("OrderCount = %d, want 100", db.OrderCount())
	}
	if db.OutboxSize() != 100 {
		t.Errorf("OutboxSize = %d, want 100", db.OutboxSize())
	}
}

// Event IDs must be unique and monotonic under concurrency, or ordering breaks.
func TestEventIDsAreUniqueAndOrdered(t *testing.T) {
	db := NewDB()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			PlaceOrder(db, fmt.Sprintf("order-%d", i), 1)
		}(i)
	}
	wg.Wait()

	events := db.UnpublishedEvents(0)
	seen := map[int64]bool{}
	var prev int64
	for _, e := range events {
		if seen[e.ID] {
			t.Fatalf("duplicate event ID %d", e.ID)
		}
		seen[e.ID] = true
		if e.ID <= prev {
			t.Fatalf("event IDs not ordered: %d after %d", e.ID, prev)
		}
		prev = e.ID
	}
	if len(events) != 200 {
		t.Errorf("got %d events, want 200", len(events))
	}
}
