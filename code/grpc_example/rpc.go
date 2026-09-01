// This file builds a working RPC server and client over TCP using the wire
// format in wire.go. It is deliberately small — the goal is to show the three
// mechanisms that matter and that gRPC provides for you:
//
//  1. FRAMING. A stream of bytes has no message boundaries, so every message is
//     length-prefixed. gRPC uses a 5-byte prefix (1 compression flag + 4 length)
//     for exactly this reason.
//  2. SERVER STREAMING. One request, many responses, ending with an explicit
//     end-of-stream marker rather than closing the connection.
//  3. STATUS CODES. Errors are structured values, not strings.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Code mirrors the gRPC status codes worth knowing.
type Code uint8

const (
	CodeOK Code = iota
	CodeNotFound
	CodeInvalidArgument
	CodeAlreadyExists
	CodeInternal
	CodeUnavailable
)

func (c Code) String() string {
	switch c {
	case CodeOK:
		return "OK"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodeInvalidArgument:
		return "INVALID_ARGUMENT"
	case CodeAlreadyExists:
		return "ALREADY_EXISTS"
	case CodeInternal:
		return "INTERNAL"
	case CodeUnavailable:
		return "UNAVAILABLE"
	}
	return "UNKNOWN"
}

type StatusError struct {
	Code    Code
	Message string
}

func (e *StatusError) Error() string { return e.Code.String() + ": " + e.Message }

// Frame kinds, so a server-streaming call can signal completion.
const (
	frameMessage byte = 0
	frameEnd     byte = 1
	frameError   byte = 2
)

// writeFrame writes kind + 4-byte big-endian length + payload.
//
// ⭐ The length prefix is the whole reason this works over a stream. Without it
// the receiver cannot tell where one message ends and the next begins.
func writeFrame(w io.Writer, kind byte, payload []byte) error {
	header := make([]byte, 5)
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	// ⚠️ Always bound the declared length. A malicious or corrupt peer otherwise
	// makes you allocate 4 GB.
	if length > 16<<20 {
		return 0, nil, errors.New("rpc: frame exceeds the 16 MB limit")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
}

// Method identifies an RPC. A real gRPC call uses an HTTP/2 path such as
// /orders.v1.OrderService/GetOrder.
type Method uint8

const (
	MethodGetOrder Method = iota
	MethodCreateOrder
	MethodWatchOrders
)

// OrderStore is the service implementation.
type OrderStore struct {
	mu     sync.Mutex
	orders map[int64]*Order
	// byIdempotencyKey makes CreateOrder safe to retry.
	byIdempotencyKey map[string]int64
	nextID           int64
}

func NewOrderStore() *OrderStore {
	return &OrderStore{
		orders:           map[int64]*Order{},
		byIdempotencyKey: map[string]int64{},
	}
}

func (s *OrderStore) GetOrder(id int64) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return nil, &StatusError{CodeNotFound, fmt.Sprintf("order %d not found", id)}
	}
	copy := *o
	return &copy, nil
}

// CreateOrder is idempotent on the key: a retried request returns the original
// order rather than creating a second one.
func (s *OrderStore) CreateOrder(customer string, amount int64, idempotencyKey string) (*Order, error) {
	if customer == "" {
		return nil, &StatusError{CodeInvalidArgument, "customer is required"}
	}
	if amount <= 0 {
		return nil, &StatusError{CodeInvalidArgument, "amount must be positive"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if idempotencyKey != "" {
		if id, ok := s.byIdempotencyKey[idempotencyKey]; ok {
			copy := *s.orders[id]
			return &copy, nil
		}
	}

	s.nextID++
	o := &Order{
		ID:       s.nextID,
		Customer: customer,
		Amount:   amount,
		Status:   StatusPending,
	}
	s.orders[o.ID] = o
	if idempotencyKey != "" {
		s.byIdempotencyKey[idempotencyKey] = o.ID
	}
	copy := *o
	return &copy, nil
}

func (s *OrderStore) ListByCustomer(customer string) []*Order {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*Order
	for id := int64(1); id <= s.nextID; id++ {
		if o, ok := s.orders[id]; ok && (customer == "" || o.Customer == customer) {
			copy := *o
			out = append(out, &copy)
		}
	}
	return out
}

// Server serves the OrderService over TCP.
type Server struct {
	store    *OrderStore
	listener net.Listener
	wg       sync.WaitGroup
	closing  chan struct{}
	once     sync.Once

	// conns tracks live connections so shutdown can force them closed.
	// ⚠️ Without this, Close blocks forever on handlers parked in a blocking
	// read waiting for a client that may never send anything again.
	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

func NewServer(store *OrderStore) *Server {
	return &Server{
		store:   store,
		closing: make(chan struct{}),
		conns:   map[net.Conn]struct{}{},
	}
}

func (s *Server) trackConn(c net.Conn) bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	select {
	case <-s.closing:
		return false
	default:
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrackConn(c net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, c)
}

// Listen binds to addr and serves until Close. Use "127.0.0.1:0" to get a free
// port, then read Addr.
func (s *Server) Listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = l

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return // listener closed, or a fatal accept error
			}
			if !s.trackConn(conn) {
				conn.Close()
				return
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer s.untrackConn(conn)
				s.handle(conn)
			}()
		}
	}()
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Close stops accepting, forcibly closes live connections so blocked reads
// return, and waits for handlers to finish.
func (s *Server) Close() {
	s.once.Do(func() {
		close(s.closing)
		if s.listener != nil {
			s.listener.Close()
		}
		s.connMu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.connMu.Unlock()
	})
	s.wg.Wait()
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		kind, payload, err := readFrame(r)
		if err != nil || kind != frameMessage || len(payload) < 1 {
			return
		}
		s.dispatch(w, Method(payload[0]), payload[1:])
		// ⚠️ Flush on every path. An earlier version returned early on the error
		// branch and skipped this, so error frames sat in the buffer and the
		// client blocked forever waiting for a reply that had been written but
		// never sent.
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(w io.Writer, method Method, body []byte) {
	switch method {
	case MethodGetOrder:
		var req Order
		if err := req.Unmarshal(body); err != nil {
			s.writeError(w, &StatusError{CodeInvalidArgument, err.Error()})
			return
		}
		o, err := s.store.GetOrder(req.ID)
		if err != nil {
			s.writeError(w, err)
			return
		}
		writeFrame(w, frameMessage, o.Marshal())

	case MethodCreateOrder:
		var req Order
		if err := req.Unmarshal(body); err != nil {
			s.writeError(w, &StatusError{CodeInvalidArgument, err.Error()})
			return
		}
		key := ""
		if len(req.Tags) > 0 {
			key = req.Tags[0] // the demo carries the idempotency key here
		}
		o, err := s.store.CreateOrder(req.Customer, req.Amount, key)
		if err != nil {
			s.writeError(w, err)
			return
		}
		writeFrame(w, frameMessage, o.Marshal())

	case MethodWatchOrders:
		var req Order
		if err := req.Unmarshal(body); err != nil {
			s.writeError(w, &StatusError{CodeInvalidArgument, err.Error()})
			return
		}
		// ⭐ Server streaming: many frames, then an explicit end marker. The
		// client can start processing before the server has finished.
		for _, o := range s.store.ListByCustomer(req.Customer) {
			if err := writeFrame(w, frameMessage, o.Marshal()); err != nil {
				return
			}
		}
		writeFrame(w, frameEnd, nil)

	default:
		s.writeError(w, &StatusError{CodeInvalidArgument, "unknown method"})
	}
}

func (s *Server) writeError(w io.Writer, err error) {
	var se *StatusError
	if !errors.As(err, &se) {
		se = &StatusError{CodeInternal, err.Error()}
	}
	payload := append([]byte{byte(se.Code)}, se.Message...)
	writeFrame(w, frameError, payload)
}

// Client is the RPC client.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	mu   sync.Mutex
}

func Dial(addr string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, &StatusError{CodeUnavailable, err.Error()}
	}
	return &Client{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(method Method, req *Order) ([]byte, error) {
	payload := append([]byte{byte(method)}, req.Marshal()...)
	if err := writeFrame(c.w, frameMessage, payload); err != nil {
		return nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}

	kind, body, err := readFrame(c.r)
	if err != nil {
		return nil, err
	}
	if kind == frameError {
		if len(body) < 1 {
			return nil, &StatusError{CodeInternal, "malformed error frame"}
		}
		return nil, &StatusError{Code(body[0]), string(body[1:])}
	}
	return body, nil
}

func (c *Client) GetOrder(id int64) (*Order, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	body, err := c.call(MethodGetOrder, &Order{ID: id})
	if err != nil {
		return nil, err
	}
	var o Order
	return &o, o.Unmarshal(body)
}

func (c *Client) CreateOrder(customer string, amount int64, idempotencyKey string) (*Order, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := &Order{Customer: customer, Amount: amount}
	if idempotencyKey != "" {
		req.Tags = []string{idempotencyKey}
	}
	body, err := c.call(MethodCreateOrder, req)
	if err != nil {
		return nil, err
	}
	var o Order
	return &o, o.Unmarshal(body)
}

// WatchOrders consumes a server stream, invoking fn for each message.
func (c *Client) WatchOrders(customer string, fn func(*Order) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	payload := append([]byte{byte(MethodWatchOrders)}, (&Order{Customer: customer}).Marshal()...)
	if err := writeFrame(c.w, frameMessage, payload); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}

	for {
		kind, body, err := readFrame(c.r)
		if err != nil {
			return err
		}
		switch kind {
		case frameEnd:
			return nil
		case frameError:
			return &StatusError{Code(body[0]), string(body[1:])}
		case frameMessage:
			var o Order
			if err := o.Unmarshal(body); err != nil {
				return err
			}
			if err := fn(&o); err != nil {
				return err
			}
		}
	}
}

func main() {
	fmt.Println("=== the wire format, byte by byte ===")
	order := &Order{
		ID:       12345,
		Customer: "alice",
		Amount:   4999,
		Status:   StatusPaid,
		Tags:     []string{"priority"},
	}
	encoded := order.Marshal()
	fmt.Printf("encoded %d bytes: % x\n\n", len(encoded), encoded)
	desc, _ := Describe(encoded)
	fmt.Print(desc)

	fmt.Println("\n=== size comparison ===")
	jsonish := `{"id":12345,"customer":"alice","amount":4999,"status":"PAID","tags":["priority"]}`
	fmt.Printf("protobuf: %d bytes\n", len(encoded))
	fmt.Printf("JSON:     %d bytes  (%.1fx larger)\n",
		len(jsonish), float64(len(jsonish))/float64(len(encoded)))
	fmt.Println("field names are absent from the wire — that is most of the difference")

	fmt.Println("\n=== zigzag: why sint64 exists ===")
	negativeOne := int64(-1)
	fmt.Printf("  -1 as a plain varint: %d bytes\n", VarintSize(uint64(negativeOne)))
	fmt.Printf("  -1 as zigzag sint64:  %d byte\n", VarintSize(ZigZag(negativeOne)))

	fmt.Println("\n=== forward compatibility ===")
	// A newer sender adds field 9, which this binary knows nothing about.
	extended := append(order.Marshal(), AppendString(nil, 9, "field-from-the-future")...)
	var decoded Order
	decoded.Unmarshal(extended)
	fmt.Printf("decoded by an old binary: id=%d customer=%s amount=%d\n",
		decoded.ID, decoded.Customer, decoded.Amount)
	fmt.Printf("unknown field preserved: %d bytes retained on re-encode\n", len(decoded.unknownFields))
	fmt.Println("⭐ round-tripping through an old binary does not destroy new fields")

	fmt.Println("\n=== a real RPC round trip ===")
	store := NewOrderStore()
	server := NewServer(store)
	if err := server.Listen("127.0.0.1:0"); err != nil {
		panic(err)
	}
	defer server.Close()

	client, err := Dial(server.Addr(), 2*time.Second)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	created, _ := client.CreateOrder("alice", 4999, "idem-1")
	fmt.Printf("created:  id=%d customer=%s amount=%d status=%s\n",
		created.ID, created.Customer, created.Amount, created.Status)

	// The same idempotency key must not create a second order.
	retried, _ := client.CreateOrder("alice", 4999, "idem-1")
	fmt.Printf("retried:  id=%d (same order, not a duplicate)\n", retried.ID)

	client.CreateOrder("alice", 1500, "idem-2")
	client.CreateOrder("bob", 2500, "idem-3")

	fetched, _ := client.GetOrder(created.ID)
	fmt.Printf("fetched:  id=%d customer=%s\n", fetched.ID, fetched.Customer)

	_, err = client.GetOrder(99999)
	fmt.Printf("missing:  %v\n", err)

	fmt.Println("\nserver-streaming WatchOrders for alice:")
	client.WatchOrders("alice", func(o *Order) error {
		fmt.Printf("  streamed id=%d amount=%d\n", o.ID, o.Amount)
		return nil
	})
}
