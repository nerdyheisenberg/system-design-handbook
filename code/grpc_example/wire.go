// Package main decodes and encodes the protobuf wire format by hand, then builds
// a small RPC server and client on top of it. See Chapter 15.
//
// This file is the wire format. There is no code generation and no external
// dependency — the point is that the format is simple enough to implement, and
// understanding it explains the design rules that otherwise look arbitrary.
//
// Every field is encoded as a TAG followed by a VALUE:
//
//	tag = (field_number << 3) | wire_type      encoded as a varint
//
//	wire type 0  varint            int32/64, uint, bool, enum
//	wire type 1  64-bit fixed      double, fixed64
//	wire type 2  length-delimited  string, bytes, embedded messages, packed
//	wire type 5  32-bit fixed      float, fixed32
//
// ⭐ Three consequences fall straight out of this layout:
//
//  1. FIELD NAMES ARE NOT ON THE WIRE. Only numbers. So renaming a field is free
//     and changing its number is a breaking change — the opposite of JSON.
//  2. UNKNOWN FIELDS ARE SKIPPABLE, because the wire type tells you the length of
//     a value you do not recognise. That is what makes forward compatibility work:
//     an old reader can parse a new message.
//  3. ABSENT AND ZERO ARE INDISTINGUISHABLE in proto3, because a zero-valued
//     field is simply not written. This is why "is this field set?" needs an
//     explicit wrapper or `optional`.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

type WireType int

const (
	WireVarint  WireType = 0
	WireFixed64 WireType = 1
	WireBytes   WireType = 2
	WireFixed32 WireType = 5
)

func (w WireType) String() string {
	switch w {
	case WireVarint:
		return "varint"
	case WireFixed64:
		return "fixed64"
	case WireBytes:
		return "length-delimited"
	case WireFixed32:
		return "fixed32"
	}
	return "unknown"
}

var (
	ErrTruncated   = errors.New("protobuf: truncated message")
	ErrBadWireType = errors.New("protobuf: unsupported wire type")
)

// AppendVarint writes a base-128 varint: seven bits of payload per byte, with the
// top bit set on every byte except the last.
//
// ⭐ This is why small numbers are cheap: field numbers 1-15 fit their whole tag
// in one byte, which is why the proto style guide tells you to reserve them for
// the fields that appear most often.
func AppendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// ConsumeVarint reads a varint and returns the number of bytes consumed.
func ConsumeVarint(b []byte) (uint64, int, error) {
	var v uint64
	var shift uint
	for i := 0; i < len(b); i++ {
		if i > 9 {
			return 0, 0, errors.New("protobuf: varint overflows 64 bits")
		}
		c := b[i]
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, i + 1, nil
		}
		shift += 7
	}
	return 0, 0, ErrTruncated
}

// VarintSize is how many bytes a value will occupy.
func VarintSize(v uint64) int {
	if v == 0 {
		return 1
	}
	return (bits.Len64(v) + 6) / 7
}

// ZigZag encodes a signed integer so that small negative numbers stay small.
//
// ⚠️ Without it, -1 as a plain varint is 0xFFFFFFFFFFFFFFFF — ten bytes. This is
// the entire reason sint32/sint64 exist as separate types from int32/int64: use
// them whenever a field can be negative.
func ZigZag(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func UnZigZag(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// AppendTag writes the field number and wire type.
func AppendTag(b []byte, fieldNumber int, wt WireType) []byte {
	return AppendVarint(b, uint64(fieldNumber)<<3|uint64(wt))
}

func ConsumeTag(b []byte) (fieldNumber int, wt WireType, n int, err error) {
	v, n, err := ConsumeVarint(b)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(v >> 3), WireType(v & 0x7), n, nil
}

func AppendInt64(b []byte, fieldNumber int, v int64) []byte {
	if v == 0 {
		return b // proto3 omits zero values entirely
	}
	b = AppendTag(b, fieldNumber, WireVarint)
	return AppendVarint(b, uint64(v))
}

func AppendString(b []byte, fieldNumber int, s string) []byte {
	if s == "" {
		return b
	}
	b = AppendTag(b, fieldNumber, WireBytes)
	b = AppendVarint(b, uint64(len(s)))
	return append(b, s...)
}

func AppendBytes(b []byte, fieldNumber int, v []byte) []byte {
	if len(v) == 0 {
		return b
	}
	b = AppendTag(b, fieldNumber, WireBytes)
	b = AppendVarint(b, uint64(len(v)))
	return append(b, v...)
}

// ConsumeBytes reads a length-delimited value.
func ConsumeBytes(b []byte) ([]byte, int, error) {
	length, n, err := ConsumeVarint(b)
	if err != nil {
		return nil, 0, err
	}
	if uint64(len(b)-n) < length {
		return nil, 0, ErrTruncated
	}
	return b[n : n+int(length)], n + int(length), nil
}

// SkipValue advances past a field the decoder does not recognise.
//
// ⭐ This single function is what makes protobuf forward compatible. Because the
// wire type encodes how to find the end of a value, an old binary can read a
// message containing fields it has never heard of, and — critically — re-serialise
// them unchanged if it preserves the raw bytes.
func SkipValue(b []byte, wt WireType) (int, error) {
	switch wt {
	case WireVarint:
		_, n, err := ConsumeVarint(b)
		return n, err
	case WireFixed64:
		if len(b) < 8 {
			return 0, ErrTruncated
		}
		return 8, nil
	case WireBytes:
		_, n, err := ConsumeBytes(b)
		return n, err
	case WireFixed32:
		if len(b) < 4 {
			return 0, ErrTruncated
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("%w: %d", ErrBadWireType, wt)
	}
}

// Status mirrors the enum in order.proto.
type Status int64

const (
	StatusUnspecified Status = 0
	StatusPending     Status = 1
	StatusPaid        Status = 2
	StatusCancelled   Status = 3
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "PENDING"
	case StatusPaid:
		return "PAID"
	case StatusCancelled:
		return "CANCELLED"
	default:
		return "UNSPECIFIED"
	}
}

// Order corresponds to the Order message in order.proto.
type Order struct {
	ID       int64
	Customer string
	Amount   int64
	Status   Status
	Tags     []string

	// unknownFields preserves fields written by a newer sender so they survive a
	// decode/encode round trip through an older binary. Dropping them is a real
	// and subtle source of data loss in proxies and gateways.
	unknownFields []byte
}

const (
	fieldOrderID       = 1
	fieldOrderCustomer = 2
	fieldOrderAmount   = 3
	fieldOrderStatus   = 4
	fieldOrderTags     = 5
)

func (o *Order) Marshal() []byte {
	var b []byte
	b = AppendInt64(b, fieldOrderID, o.ID)
	b = AppendString(b, fieldOrderCustomer, o.Customer)
	b = AppendInt64(b, fieldOrderAmount, o.Amount)
	b = AppendInt64(b, fieldOrderStatus, int64(o.Status))
	for _, t := range o.Tags {
		b = AppendString(b, fieldOrderTags, t)
	}
	return append(b, o.unknownFields...)
}

func (o *Order) Unmarshal(b []byte) error {
	*o = Order{}
	for len(b) > 0 {
		field, wt, n, err := ConsumeTag(b)
		if err != nil {
			return err
		}
		rest := b[n:]

		switch {
		case field == fieldOrderID && wt == WireVarint:
			v, m, err := ConsumeVarint(rest)
			if err != nil {
				return err
			}
			o.ID = int64(v)
			b = rest[m:]

		case field == fieldOrderCustomer && wt == WireBytes:
			v, m, err := ConsumeBytes(rest)
			if err != nil {
				return err
			}
			o.Customer = string(v)
			b = rest[m:]

		case field == fieldOrderAmount && wt == WireVarint:
			v, m, err := ConsumeVarint(rest)
			if err != nil {
				return err
			}
			o.Amount = int64(v)
			b = rest[m:]

		case field == fieldOrderStatus && wt == WireVarint:
			v, m, err := ConsumeVarint(rest)
			if err != nil {
				return err
			}
			o.Status = Status(v)
			b = rest[m:]

		case field == fieldOrderTags && wt == WireBytes:
			v, m, err := ConsumeBytes(rest)
			if err != nil {
				return err
			}
			o.Tags = append(o.Tags, string(v))
			b = rest[m:]

		default:
			// Unknown field: skip it, but keep the raw bytes.
			m, err := SkipValue(rest, wt)
			if err != nil {
				return err
			}
			o.unknownFields = append(o.unknownFields, b[:n+m]...)
			b = rest[m:]
		}
	}
	return nil
}

// Describe renders the encoding field by field, which is the point of writing
// this by hand: you can see exactly where every byte went.
func Describe(b []byte) (string, error) {
	out := ""
	for len(b) > 0 {
		field, wt, n, err := ConsumeTag(b)
		if err != nil {
			return out, err
		}
		tagBytes := b[:n]
		rest := b[n:]

		m, err := SkipValue(rest, wt)
		if err != nil {
			return out, err
		}
		value := rest[:m]

		out += fmt.Sprintf("  field %d (%s)\n    tag:   % x\n    value: % x",
			field, wt, tagBytes, value)
		switch wt {
		case WireVarint:
			v, _, _ := ConsumeVarint(value)
			out += fmt.Sprintf("  = %d", v)
		case WireBytes:
			v, _, _ := ConsumeBytes(value)
			out += fmt.Sprintf("  = %q", string(v))
		}
		out += "\n"
		b = rest[m:]
	}
	return out, nil
}

// appendFixed64 and appendFixed32 exist so the tests can build messages
// containing wire types the Order decoder does not use, proving SkipValue works.
func appendFixed64(b []byte, fieldNumber int, v uint64) []byte {
	b = AppendTag(b, fieldNumber, WireFixed64)
	return binary.LittleEndian.AppendUint64(b, v)
}

func appendFixed32(b []byte, fieldNumber int, v uint32) []byte {
	b = AppendTag(b, fieldNumber, WireFixed32)
	return binary.LittleEndian.AppendUint32(b, v)
}
