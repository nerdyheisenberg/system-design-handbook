package main

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestVarintRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 127, 128, 255, 300, 16383, 16384, 1 << 32, ^uint64(0)}
	for _, v := range values {
		b := AppendVarint(nil, v)
		got, n, err := ConsumeVarint(b)
		if err != nil {
			t.Fatalf("%d: %v", v, err)
		}
		if got != v {
			t.Errorf("round trip of %d gave %d", v, got)
		}
		if n != len(b) {
			t.Errorf("%d: consumed %d of %d bytes", v, n, len(b))
		}
	}
}

// Known encodings from the protobuf specification.
func TestVarintKnownEncodings(t *testing.T) {
	cases := map[uint64][]byte{
		0:   {0x00},
		1:   {0x01},
		127: {0x7f},
		128: {0x80, 0x01},
		300: {0xac, 0x02},
	}
	for v, want := range cases {
		got := AppendVarint(nil, v)
		if fmt.Sprintf("% x", got) != fmt.Sprintf("% x", want) {
			t.Errorf("AppendVarint(%d) = % x, want % x", v, got, want)
		}
	}
}

// ⭐ Small numbers are cheap, which is why low field numbers matter.
func TestVarintSizeGrowsWithMagnitude(t *testing.T) {
	cases := map[uint64]int{0: 1, 127: 1, 128: 2, 16383: 2, 16384: 3, ^uint64(0): 10}
	for v, want := range cases {
		if got := VarintSize(v); got != want {
			t.Errorf("VarintSize(%d) = %d, want %d", v, got, want)
		}
		if got := len(AppendVarint(nil, v)); got != want {
			t.Errorf("encoded length of %d = %d, want %d", v, got, want)
		}
	}
}

func TestTruncatedVarintIsDetected(t *testing.T) {
	// Every byte has the continuation bit set, so the value never terminates.
	if _, _, err := ConsumeVarint([]byte{0x80, 0x80, 0x80}); !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

func TestOverlongVarintIsRejected(t *testing.T) {
	b := make([]byte, 12)
	for i := range b {
		b[i] = 0x80
	}
	if _, _, err := ConsumeVarint(b); err == nil {
		t.Error("a varint longer than 10 bytes should be rejected")
	}
}

// ⚠️ The reason sint64 exists: -1 costs 10 bytes as a plain varint, 1 as zigzag.
func TestZigZagKeepsNegativesSmall(t *testing.T) {
	negativeOne := int64(-1)
	if got := VarintSize(uint64(negativeOne)); got != 10 {
		t.Errorf("plain varint size of -1 = %d, want 10", got)
	}
	if got := VarintSize(ZigZag(negativeOne)); got != 1 {
		t.Errorf("zigzag size of -1 = %d, want 1", got)
	}
}

func TestZigZagRoundTrip(t *testing.T) {
	for _, v := range []int64{0, -1, 1, -2, 2, -64, 63, -1000000, 1000000, 1 << 62, -(1 << 62)} {
		if got := UnZigZag(ZigZag(v)); got != v {
			t.Errorf("round trip of %d gave %d", v, got)
		}
	}
}

func TestZigZagKnownValues(t *testing.T) {
	cases := map[int64]uint64{0: 0, -1: 1, 1: 2, -2: 3, 2: 4}
	for in, want := range cases {
		if got := ZigZag(in); got != want {
			t.Errorf("ZigZag(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestTagEncodesFieldAndWireType(t *testing.T) {
	b := AppendTag(nil, 5, WireBytes)
	field, wt, n, err := ConsumeTag(b)
	if err != nil {
		t.Fatal(err)
	}
	if field != 5 || wt != WireBytes || n != len(b) {
		t.Errorf("got field=%d wt=%v n=%d", field, wt, n)
	}
}

// Fields 1-15 fit their tag in one byte; 16+ need two. This is the reason the
// style guide reserves low numbers for frequently-set fields.
func TestLowFieldNumbersHaveOneByteTags(t *testing.T) {
	for f := 1; f <= 15; f++ {
		if got := len(AppendTag(nil, f, WireVarint)); got != 1 {
			t.Errorf("field %d tag is %d bytes, want 1", f, got)
		}
	}
	for f := 16; f <= 100; f++ {
		if got := len(AppendTag(nil, f, WireVarint)); got != 2 {
			t.Errorf("field %d tag is %d bytes, want 2", f, got)
		}
	}
}

func TestOrderRoundTrip(t *testing.T) {
	original := &Order{
		ID:       12345,
		Customer: "alice",
		Amount:   4999,
		Status:   StatusPaid,
		Tags:     []string{"priority", "gift"},
	}

	var decoded Order
	if err := decoded.Unmarshal(original.Marshal()); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != original.ID || decoded.Customer != original.Customer ||
		decoded.Amount != original.Amount || decoded.Status != original.Status {
		t.Errorf("decoded = %+v, want %+v", decoded, original)
	}
	if len(decoded.Tags) != 2 || decoded.Tags[0] != "priority" || decoded.Tags[1] != "gift" {
		t.Errorf("Tags = %v, want [priority gift]", decoded.Tags)
	}
}

// ⚠️ proto3 omits zero values, so absent and zero are indistinguishable. This
// test documents the behaviour rather than treating it as a bug.
func TestZeroValuesAreNotEncoded(t *testing.T) {
	empty := &Order{}
	if got := len(empty.Marshal()); got != 0 {
		t.Errorf("an all-zero message encoded to %d bytes, want 0", got)
	}

	var decoded Order
	if err := decoded.Unmarshal(nil); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != 0 || decoded.Customer != "" {
		t.Error("decoding an empty message should give zero values")
	}
}

func TestEmptyStringIsOmitted(t *testing.T) {
	withEmpty := &Order{ID: 1, Customer: ""}
	without := &Order{ID: 1}
	if len(withEmpty.Marshal()) != len(without.Marshal()) {
		t.Error("an empty string should encode identically to an absent field")
	}
}

// ⭐ Forward compatibility: an old decoder must skip fields it does not know.
func TestUnknownFieldsAreSkipped(t *testing.T) {
	b := (&Order{ID: 1, Customer: "alice"}).Marshal()
	b = AppendString(b, 99, "a field from a newer schema")
	b = AppendInt64(b, 50, 12345)

	var decoded Order
	if err := decoded.Unmarshal(b); err != nil {
		t.Fatalf("an old decoder failed on a newer message: %v", err)
	}
	if decoded.ID != 1 || decoded.Customer != "alice" {
		t.Error("known fields were not decoded correctly")
	}
}

// Dropping unknown fields silently loses data when a message round-trips through
// an intermediary — a real failure mode in gateways and proxies.
func TestUnknownFieldsSurviveReEncoding(t *testing.T) {
	original := (&Order{ID: 1, Customer: "alice"}).Marshal()
	extended := AppendString(original, 99, "future-value")

	var decoded Order
	if err := decoded.Unmarshal(extended); err != nil {
		t.Fatal(err)
	}
	reEncoded := decoded.Marshal()

	var again Order
	if err := again.Unmarshal(reEncoded); err != nil {
		t.Fatal(err)
	}
	if len(again.unknownFields) == 0 {
		t.Error("the unknown field was lost on re-encode")
	}
	if again.ID != 1 || again.Customer != "alice" {
		t.Error("known fields were corrupted by the round trip")
	}
}

// SkipValue must handle every wire type, including ones this message never uses.
func TestSkipValueHandlesAllWireTypes(t *testing.T) {
	b := (&Order{ID: 7}).Marshal()
	b = appendFixed64(b, 20, 0xdeadbeefcafebabe)
	b = appendFixed32(b, 21, 0x12345678)
	b = AppendBytes(b, 22, []byte{1, 2, 3, 4, 5})
	b = AppendInt64(b, 23, 999999)

	var decoded Order
	if err := decoded.Unmarshal(b); err != nil {
		t.Fatalf("failed to skip a wire type: %v", err)
	}
	if decoded.ID != 7 {
		t.Errorf("ID = %d, want 7", decoded.ID)
	}
}

// Truncating mid-value must be caught rather than silently decoding short.
func TestTruncatedMessageIsRejected(t *testing.T) {
	full := (&Order{ID: 1, Customer: "alice-with-a-long-name", Amount: 100}).Marshal()

	// Cut inside the length-delimited customer field.
	if len(full) < 10 {
		t.Fatal("test setup: message too short")
	}
	var decoded Order
	if err := decoded.Unmarshal(full[:8]); err == nil {
		t.Error("a message truncated mid-value should fail to decode")
	}
}

func TestLengthDelimitedTruncationIsCaught(t *testing.T) {
	b := AppendTag(nil, 2, WireBytes)
	b = AppendVarint(b, 100) // claims 100 bytes
	b = append(b, "short"...)

	var decoded Order
	if err := decoded.Unmarshal(b); !errors.Is(err, ErrTruncated) {
		t.Errorf("err = %v, want ErrTruncated", err)
	}
}

// The size argument for protobuf over JSON, made concrete.
func TestEncodingIsCompact(t *testing.T) {
	order := &Order{ID: 12345, Customer: "alice", Amount: 4999, Status: StatusPaid}
	encoded := len(order.Marshal())
	jsonish := len(`{"id":12345,"customer":"alice","amount":4999,"status":"PAID"}`)

	if encoded >= jsonish {
		t.Errorf("protobuf %d bytes vs JSON %d — expected protobuf to be smaller", encoded, jsonish)
	}
}

func TestStatusEnumString(t *testing.T) {
	for s, want := range map[Status]string{
		StatusUnspecified: "UNSPECIFIED", StatusPending: "PENDING",
		StatusPaid: "PAID", StatusCancelled: "CANCELLED",
	} {
		if s.String() != want {
			t.Errorf("String() = %q, want %q", s.String(), want)
		}
	}
}

func TestDescribeProducesOutput(t *testing.T) {
	order := &Order{ID: 1, Customer: "alice", Amount: 100}
	out, err := Describe(order.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("Describe returned nothing")
	}
}

// --- RPC tests ---

func newTestServer(t *testing.T) (*Server, *Client) {
	t.Helper()
	server := NewServer(NewOrderStore())
	if err := server.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	client, err := Dial(server.Addr(), 2*time.Second)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return server, client
}

func TestRPCCreateAndGet(t *testing.T) {
	_, client := newTestServer(t)

	created, err := client.CreateOrder("alice", 4999, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Customer != "alice" || created.Amount != 4999 {
		t.Errorf("created = %+v", created)
	}
	if created.Status != StatusPending {
		t.Errorf("Status = %v, want PENDING", created.Status)
	}

	fetched, err := client.GetOrder(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.ID != created.ID || fetched.Customer != "alice" {
		t.Errorf("fetched = %+v, want %+v", fetched, created)
	}
}

func TestRPCNotFoundReturnsStatusCode(t *testing.T) {
	_, client := newTestServer(t)

	_, err := client.GetOrder(99999)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T, want *StatusError", err)
	}
	if se.Code != CodeNotFound {
		t.Errorf("Code = %v, want NOT_FOUND", se.Code)
	}
}

func TestRPCValidationErrors(t *testing.T) {
	_, client := newTestServer(t)

	for _, c := range []struct {
		customer string
		amount   int64
	}{
		{"", 100},
		{"alice", 0},
		{"alice", -5},
	} {
		_, err := client.CreateOrder(c.customer, c.amount, "")
		var se *StatusError
		if !errors.As(err, &se) || se.Code != CodeInvalidArgument {
			t.Errorf("CreateOrder(%q, %d) err = %v, want INVALID_ARGUMENT", c.customer, c.amount, err)
		}
	}
}

// A retried create with the same key must return the original order.
func TestRPCCreateIsIdempotent(t *testing.T) {
	_, client := newTestServer(t)

	first, err := client.CreateOrder("alice", 4999, "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := client.CreateOrder("alice", 4999, "idem-1")
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != first.ID {
			t.Fatalf("retry created a new order %d, want %d", again.ID, first.ID)
		}
	}
}

func TestRPCDifferentKeysCreateDifferentOrders(t *testing.T) {
	_, client := newTestServer(t)

	a, _ := client.CreateOrder("alice", 100, "key-a")
	b, _ := client.CreateOrder("alice", 100, "key-b")
	if a.ID == b.ID {
		t.Error("different idempotency keys should create different orders")
	}
}

// ⭐ Server streaming: one request, many responses, then an end marker.
func TestRPCServerStreaming(t *testing.T) {
	_, client := newTestServer(t)

	for i := 1; i <= 5; i++ {
		client.CreateOrder("alice", int64(i*100), fmt.Sprintf("key-%d", i))
	}
	client.CreateOrder("bob", 999, "key-bob")

	var received []*Order
	if err := client.WatchOrders("alice", func(o *Order) error {
		received = append(received, o)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(received) != 5 {
		t.Fatalf("received %d orders, want 5", len(received))
	}
	for i, o := range received {
		if o.Customer != "alice" {
			t.Errorf("order %d belongs to %q", i, o.Customer)
		}
		if want := int64((i + 1) * 100); o.Amount != want {
			t.Errorf("order %d amount = %d, want %d — stream order is wrong", i, o.Amount, want)
		}
	}
}

func TestRPCStreamingEmptyResult(t *testing.T) {
	_, client := newTestServer(t)

	count := 0
	if err := client.WatchOrders("nobody", func(*Order) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("received %d orders, want 0", count)
	}
}

// Framing must survive many messages on one connection — the whole reason for
// the length prefix.
func TestRPCManySequentialCallsOnOneConnection(t *testing.T) {
	_, client := newTestServer(t)

	for i := 1; i <= 200; i++ {
		created, err := client.CreateOrder("alice", int64(i), fmt.Sprintf("key-%d", i))
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		fetched, err := client.GetOrder(created.ID)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
		if fetched.Amount != int64(i) {
			t.Fatalf("call %d: amount = %d — framing has desynchronised", i, fetched.Amount)
		}
	}
}

func TestRPCLargePayloadRoundTrip(t *testing.T) {
	_, client := newTestServer(t)

	long := ""
	for i := 0; i < 10000; i++ {
		long += "x"
	}
	created, err := client.CreateOrder(long, 100, "big")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Customer) != 10000 {
		t.Errorf("customer length = %d, want 10000", len(created.Customer))
	}
}

func TestDialUnavailable(t *testing.T) {
	_, err := Dial("127.0.0.1:1", 200*time.Millisecond)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != CodeUnavailable {
		t.Errorf("err = %v, want UNAVAILABLE", err)
	}
}

func TestCodeString(t *testing.T) {
	for c, want := range map[Code]string{
		CodeOK: "OK", CodeNotFound: "NOT_FOUND", CodeInvalidArgument: "INVALID_ARGUMENT",
		CodeAlreadyExists: "ALREADY_EXISTS", CodeInternal: "INTERNAL", CodeUnavailable: "UNAVAILABLE",
	} {
		if c.String() != want {
			t.Errorf("String() = %q, want %q", c.String(), want)
		}
	}
}

func BenchmarkMarshal(b *testing.B) {
	o := &Order{ID: 12345, Customer: "alice", Amount: 4999, Status: StatusPaid,
		Tags: []string{"priority"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o.Marshal()
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	data := (&Order{ID: 12345, Customer: "alice", Amount: 4999, Status: StatusPaid,
		Tags: []string{"priority"}}).Marshal()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var o Order
		o.Unmarshal(data)
	}
}
