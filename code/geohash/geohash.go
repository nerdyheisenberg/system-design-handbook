// Package main implements geohash encoding, decoding and neighbour lookup.
// See Chapter 23.
//
// A geohash interleaves the bits of latitude and longitude and base32-encodes the
// result, so a shared string prefix means spatial proximity. That is genuinely
// useful: it turns "find things near me" into a prefix scan on an ordinary B-tree
// index, in any database, with no spatial extension.
//
// ⚠️ But the converse is false, and that is the trap. Two points can be metres
// apart and share no prefix at all, because a cell boundary runs between them.
// The prime meridian and the equator are the worst cases. Any correct proximity
// search must therefore query the cell AND its eight neighbours — which is what
// Neighbors exists for. This limitation is a large part of why Uber built H3.
package main

import (
	"fmt"
	"math"
	"strings"
)

const base32 = "0123456789bcdefghjkmnpqrstuvwxyz" // no a, i, l, o

var decodeMap = func() map[byte]int {
	m := make(map[byte]int, len(base32))
	for i := 0; i < len(base32); i++ {
		m[base32[i]] = i
	}
	return m
}()

type Box struct {
	MinLat, MaxLat float64
	MinLng, MaxLng float64
}

func (b Box) Center() (lat, lng float64) {
	return (b.MinLat + b.MaxLat) / 2, (b.MinLng + b.MaxLng) / 2
}

// Encode produces a geohash of the given precision. Each character adds 5 bits,
// so precision trades string length against cell size:
//
//	precision 4 ≈ 39 km · 5 ≈ 4.9 km · 6 ≈ 1.2 km · 7 ≈ 153 m · 8 ≈ 38 m
func Encode(lat, lng float64, precision int) string {
	if precision < 1 {
		precision = 1
	}
	latRange := [2]float64{-90, 90}
	lngRange := [2]float64{-180, 180}

	var sb strings.Builder
	sb.Grow(precision)

	bit, ch := 0, 0
	even := true // longitude first

	for sb.Len() < precision {
		if even {
			mid := (lngRange[0] + lngRange[1]) / 2
			if lng >= mid {
				ch = ch<<1 | 1
				lngRange[0] = mid
			} else {
				ch <<= 1
				lngRange[1] = mid
			}
		} else {
			mid := (latRange[0] + latRange[1]) / 2
			if lat >= mid {
				ch = ch<<1 | 1
				latRange[0] = mid
			} else {
				ch <<= 1
				latRange[1] = mid
			}
		}
		even = !even

		if bit++; bit == 5 {
			sb.WriteByte(base32[ch])
			bit, ch = 0, 0
		}
	}
	return sb.String()
}

// Decode returns the bounding box a geohash represents. A geohash is a cell, not
// a point — treating it as a point is a common source of error.
func Decode(hash string) (Box, error) {
	box := Box{MinLat: -90, MaxLat: 90, MinLng: -180, MaxLng: 180}
	even := true

	for i := 0; i < len(hash); i++ {
		v, ok := decodeMap[hash[i]]
		if !ok {
			return Box{}, fmt.Errorf("geohash: invalid character %q", hash[i])
		}
		for mask := 16; mask >= 1; mask >>= 1 {
			set := v&mask != 0
			if even {
				mid := (box.MinLng + box.MaxLng) / 2
				if set {
					box.MinLng = mid
				} else {
					box.MaxLng = mid
				}
			} else {
				mid := (box.MinLat + box.MaxLat) / 2
				if set {
					box.MinLat = mid
				} else {
					box.MaxLat = mid
				}
			}
			even = !even
		}
	}
	return box, nil
}

// DecodePoint returns the centre of the cell, with the maximum error in each axis.
func DecodePoint(hash string) (lat, lng, latErr, lngErr float64, err error) {
	box, err := Decode(hash)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	lat, lng = box.Center()
	return lat, lng, (box.MaxLat - box.MinLat) / 2, (box.MaxLng - box.MinLng) / 2, nil
}

// Neighbors returns the eight surrounding cells at the same precision.
//
// ⭐ This is the function that makes geohash proximity search correct. Searching
// only the query point's own cell misses anything just across a boundary, and
// boundaries are invisible in the data.
func Neighbors(hash string) ([]string, error) {
	box, err := Decode(hash)
	if err != nil {
		return nil, err
	}
	lat, lng := box.Center()
	latStep := box.MaxLat - box.MinLat
	lngStep := box.MaxLng - box.MinLng

	out := make([]string, 0, 8)
	for dLat := 1; dLat >= -1; dLat-- {
		for dLng := -1; dLng <= 1; dLng++ {
			if dLat == 0 && dLng == 0 {
				continue
			}
			nLat := clampLat(lat + float64(dLat)*latStep)
			nLng := wrapLng(lng + float64(dLng)*lngStep)
			out = append(out, Encode(nLat, nLng, len(hash)))
		}
	}
	return out, nil
}

// SearchArea returns the cell containing the point plus its eight neighbours —
// the set you must scan for a correct proximity query.
func SearchArea(lat, lng float64, precision int) ([]string, error) {
	center := Encode(lat, lng, precision)
	neighbors, err := Neighbors(center)
	if err != nil {
		return nil, err
	}
	return append([]string{center}, neighbors...), nil
}

func clampLat(lat float64) float64 {
	return math.Max(-90, math.Min(90, lat))
}

func wrapLng(lng float64) float64 {
	for lng > 180 {
		lng -= 360
	}
	for lng < -180 {
		lng += 360
	}
	return lng
}

// Haversine returns the great-circle distance in metres, for verifying that
// prefix proximity actually corresponds to physical proximity.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadius = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadius * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// CommonPrefix is the length of the shared prefix of two geohashes.
func CommonPrefix(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func main() {
	const lat, lng = 51.5074, -0.1278 // Trafalgar Square

	fmt.Println("precision vs cell size:")
	for p := 3; p <= 9; p++ {
		h := Encode(lat, lng, p)
		box, _ := Decode(h)
		height := Haversine(box.MinLat, lng, box.MaxLat, lng)
		width := Haversine(lat, box.MinLng, lat, box.MaxLng)
		fmt.Printf("  %d: %-10s ~%7.0f m x %7.0f m\n", p, h, width, height)
	}

	h := Encode(lat, lng, 7)
	dLat, dLng, latErr, lngErr, _ := DecodePoint(h)
	fmt.Printf("\n%s decodes to %.4f, %.4f (+/- %.4f, %.4f)\n", h, dLat, dLng, latErr, lngErr)
	fmt.Printf("error from the original point: %.0f m\n", Haversine(lat, lng, dLat, dLng))

	neighbors, _ := Neighbors(h)
	fmt.Printf("\nthe 8 neighbours of %s:\n  %v\n", h, neighbors)

	fmt.Println("\n⚠️ the boundary problem — two points 30 m apart:")
	// Points either side of a cell boundary near the prime meridian.
	a, b := Encode(51.5074, -0.0001, 7), Encode(51.5074, 0.0001, 7)
	fmt.Printf("  %s and %s share a %d-character prefix\n", a, b, CommonPrefix(a, b))
	fmt.Printf("  actual distance: %.0f m\n", Haversine(51.5074, -0.0001, 51.5074, 0.0001))
	fmt.Println("  a prefix-only search would miss this pair entirely —")
	fmt.Println("  which is why you must always search the 8 neighbours too")

	area, _ := SearchArea(lat, lng, 6)
	fmt.Printf("\nsearch area at precision 6 is %d cells: %v\n", len(area), area)
}
