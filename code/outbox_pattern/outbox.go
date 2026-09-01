// Package main implements the transactional outbox. See Chapter 10.
//
// The problem it solves is the DUAL WRITE. An order service must (a) persist the
// order and (b) publish an OrderCreated event. Doing both directly is unsafe
// however you order them:
//
//	commit DB, then publish  → crash in between leaves an order nobody hears about
//	publish, then commit DB  → crash in between announces an order that does not exist
//
// There is no ordering that fixes this, because the database and the broker are
// separate systems with no shared transaction. Two-phase commit would work but
// blocks on coordinator failure and most brokers do not support it.
//
// ⭐ The outbox makes the event part of the SAME transaction as the state change:
// insert the row and the event atomically, then have a separate relay publish
// from the outbox table. Now the two can never disagree — either both are
// committed or neither is.
//
// The cost: delivery becomes AT-LEAST-ONCE, because the relay can crash after
// publishing but before marking the row sent. Consumers must be idempotent —
// see the idempotency_keys package.
package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DB is a minimal transactional store, standing in for PostgreSQL so this runs
// with no external dependency. The important part is that Tx spans both tables.
type DB struct {
	mu         sync.Mutex
	orders     map[string]Order
	outbox     map[int64]*OutboxEvent
	nextID     int64
	now        func() time.Time
	failCommit bool // test hook: simulate a crash at commit time
}

type Order struct {
	ID     string
	Amount int64
	Status string
}

type OutboxEvent struct {
	ID          int64
	AggregateID string
	Type        string
	Payload     string
	CreatedAt   time.Time
	// PublishedAt is nil until the relay has confirmed the broker accepted it.
	PublishedAt *time.Time
	Attempts    int
	LastError   string
}

func NewDB() *DB {
	return &DB{
		orders: map[string]Order{},
		outbox: map[int64]*OutboxEvent{},
		now:    time.Now,
	}
}

// Tx is a transaction over both the business table and the outbox.
type Tx struct {
	db     *DB
	orders []Order
	events []*OutboxEvent
	done   bool
}

var ErrCommitFailed = errors.New("db: commit failed")

func (db *DB) Begin() *Tx { return &Tx{db: db} }

func (tx *Tx) InsertOrder(o Order) { tx.orders = append(tx.orders, o) }

// EnqueueEvent stages an event in the outbox as part of this transaction.
func (tx *Tx) EnqueueEvent(aggregateID, eventType, payload string) {
	tx.events = append(tx.events, &OutboxEvent{
		AggregateID: aggregateID,
		Type:        eventType,
		Payload:     payload,
	})
}

// Commit applies the order and its events atomically. ⭐ This atomicity is the
// entire mechanism: there is no window in which one exists without the other.
func (tx *Tx) Commit() error {
	if tx.done {
		return errors.New("db: transaction already finished")
	}
	tx.done = true

	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()

	if tx.db.failCommit {
		return ErrCommitFailed
	}
	for _, o := range tx.orders {
		tx.db.orders[o.ID] = o
	}
	for _, e := range tx.events {
		tx.db.nextID++
		e.ID = tx.db.nextID
		e.CreatedAt = tx.db.now()
		tx.db.outbox[e.ID] = e
	}
	return nil
}

func (tx *Tx) Rollback() { tx.done = true }

func (db *DB) GetOrder(id string) (Order, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	o, ok := db.orders[id]
	return o, ok
}

func (db *DB) OrderCount() int {
	db.mu.Lock()
	defer db.mu.Unlock()
	return len(db.orders)
}

// UnpublishedEvents returns pending events in insertion order, which preserves
// per-aggregate ordering.
func (db *DB) UnpublishedEvents(limit int) []*OutboxEvent {
	db.mu.Lock()
	defer db.mu.Unlock()

	var out []*OutboxEvent
	for _, e := range db.outbox {
		if e.PublishedAt == nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (db *DB) MarkPublished(id int64) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if e, ok := db.outbox[id]; ok {
		t := db.now()
		e.PublishedAt = &t
	}
}

func (db *DB) RecordFailure(id int64, err error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if e, ok := db.outbox[id]; ok {
		e.Attempts++
		e.LastError = err.Error()
	}
}

func (db *DB) OutboxSize() int {
	db.mu.Lock()
	defer db.mu.Unlock()
	return len(db.outbox)
}

// Broker is the message broker the relay publishes to.
type Broker interface {
	Publish(ctx context.Context, e *OutboxEvent) error
}

// RecordingBroker captures published events, and can be told to fail.
type RecordingBroker struct {
	mu        sync.Mutex
	Published []OutboxEvent
	FailNext  int
	FailErr   error
}

func (b *RecordingBroker) Publish(ctx context.Context, e *OutboxEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.FailNext > 0 {
		b.FailNext--
		if b.FailErr != nil {
			return b.FailErr
		}
		return errors.New("broker unavailable")
	}
	b.Published = append(b.Published, *e)
	return nil
}

func (b *RecordingBroker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.Published)
}

// Types returns the published event types in order, for asserting ordering.
func (b *RecordingBroker) Types() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.Published))
	for i, e := range b.Published {
		out[i] = e.Type
	}
	return out
}

// Relay polls the outbox and publishes. In production this is either a poller
// like this one or, better, change data capture reading the write-ahead log —
// which avoids the polling latency and the load of repeated queries.
type Relay struct {
	DB     *DB
	Broker Broker
	Batch  int
}

// RunOnce publishes one batch. Returns how many were published.
//
// ⚠️ Publish-then-mark is deliberate. Marking first would lose events if the
// publish failed. Publishing first means a crash between the two re-delivers,
// which is at-least-once — the correct trade, since duplicates are recoverable
// by an idempotent consumer and lost events are not.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	events := r.DB.UnpublishedEvents(r.Batch)
	published := 0

	for _, e := range events {
		if err := ctx.Err(); err != nil {
			return published, err
		}
		if err := r.Broker.Publish(ctx, e); err != nil {
			r.DB.RecordFailure(e.ID, err)
			// Stop on failure to preserve ordering; a later event must not
			// overtake an earlier one for the same aggregate.
			return published, err
		}
		r.DB.MarkPublished(e.ID)
		published++
	}
	return published, nil
}

// RunUntilDrained repeatedly publishes until the outbox is empty or ctx ends.
func (r *Relay) RunUntilDrained(ctx context.Context, maxRounds int) (int, error) {
	total := 0
	for i := 0; i < maxRounds; i++ {
		n, err := r.RunOnce(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, nil
		}
	}
	return total, nil
}

// PlaceOrder is the application operation: one transaction, both effects.
func PlaceOrder(db *DB, id string, amount int64) error {
	tx := db.Begin()
	tx.InsertOrder(Order{ID: id, Amount: amount, Status: "created"})
	tx.EnqueueEvent(id, "OrderCreated", fmt.Sprintf(`{"order_id":%q,"amount":%d}`, id, amount))
	return tx.Commit()
}

func main() {
	fmt.Println("=== happy path ===")
	db := NewDB()
	broker := &RecordingBroker{}
	relay := &Relay{DB: db, Broker: broker, Batch: 10}

	for i := 1; i <= 3; i++ {
		PlaceOrder(db, fmt.Sprintf("order-%d", i), int64(i*100))
	}
	fmt.Printf("orders: %d, outbox rows: %d, published: %d\n",
		db.OrderCount(), db.OutboxSize(), broker.Count())

	relay.RunUntilDrained(context.Background(), 10)
	fmt.Printf("after the relay ran: published %d events\n", broker.Count())

	fmt.Println("\n=== the broker is down ===")
	db2 := NewDB()
	broker2 := &RecordingBroker{FailNext: 100}
	relay2 := &Relay{DB: db2, Broker: broker2, Batch: 10}

	PlaceOrder(db2, "order-x", 500)
	relay2.RunOnce(context.Background())

	order, _ := db2.GetOrder("order-x")
	fmt.Printf("order persisted: %+v\n", order)
	fmt.Printf("events published: %d, still pending: %d\n",
		broker2.Count(), len(db2.UnpublishedEvents(0)))
	fmt.Println("⭐ the order is safe and the event is not lost — it will be retried")

	broker2.FailNext = 0
	relay2.RunUntilDrained(context.Background(), 10)
	fmt.Printf("after recovery: published %d, pending %d\n",
		broker2.Count(), len(db2.UnpublishedEvents(0)))

	fmt.Println("\n=== the database transaction fails ===")
	db3 := NewDB()
	db3.failCommit = true
	err := PlaceOrder(db3, "order-y", 999)
	fmt.Printf("commit error: %v\n", err)
	fmt.Printf("orders: %d, outbox rows: %d\n", db3.OrderCount(), db3.OutboxSize())
	fmt.Println("⭐ neither exists — no phantom event for an order that was never created")
}
