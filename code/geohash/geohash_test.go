package main

import (
	"math"
	"testing"
)

// Reference values from the original geohash specification.
func TestKnownEncodings(t *testing.T) {
	cases := []struct {
		lat, lng  float64
		precision int
		want      string
	}{
		{57.64911, 10.40744, 11, "u4pruydqqvj"},
		{0, 0, 5, "s0000"},
		{-90, -180, 5, "00000"},
		{90, 180, 5, "zzzzz"},
	}
	for _, c := range cases {
		if got := Encode(c.lat, c.lng, c.precision); got != c.want {
			t.Errorf("Encode(%v,%v,%d) = %q, want %q", c.lat, c.lng, c.precision, got, c.want)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	points := []struct{ lat, lng float64 }{
		{51.5074, -0.1278},   // London
		{40.7128, -74.0060},  // New York
		{-33.8688, 151.2093}, // Sydney
		{35.6762, 139.6503},  // Tokyo
		{0, 0},               // null island
		{-45.5, -60.25},
	}
	for _, p := range points {
		h := Encode(p.lat, p.lng, 9)
		gotLat, gotLng, _, _, err := DecodePoint(h)
		if err != nil {
			t.Fatal(err)
		}
		if d := Haversine(p.lat, p.lng, gotLat, gotLng); d > 5 {
			t.Errorf("round trip for %v,%v drifted %.1f m", p.lat, p.lng, d)
		}
	}
}

// The property that makes geohash useful: more precision, smaller cell.
func TestPrecisionShrinksTheCell(t *testing.T) {
	const lat, lng = 51.5074, -0.1278
	prevArea := math.Inf(1)

	for p := 1; p <= 10; p++ {
		box, err := Decode(Encode(lat, lng, p))
		if err != nil {
			t.Fatal(err)
		}
		area := (box.MaxLat - box.MinLat) * (box.MaxLng - box.MinLng)
		if area >= prevArea {
			t.Errorf("precision %d has area %.6f, not smaller than %.6f", p, area, prevArea)
		}
		prevArea = area
	}
}

func TestDecodedBoxContainsThePoint(t *testing.T) {
	const lat, lng = 51.5074, -0.1278
	for p := 1; p <= 12; p++ {
		box, _ := Decode(Encode(lat, lng, p))
		if lat < box.MinLat || lat > box.MaxLat {
			t.Errorf("precision %d: latitude outside its own cell", p)
		}
		if lng < box.MinLng || lng > box.MaxLng {
			t.Errorf("precision %d: longitude outside its own cell", p)
		}
	}
}

// A shared prefix must imply proximity — this is the direction that holds.
func TestSharedPrefixImpliesProximity(t *testing.T) {
	const lat, lng = 51.5074, -0.1278
	base := Encode(lat, lng, 6)

	// Points within the same precision-6 cell (~1.2 km) must share the prefix.
	for _, d := range []float64{0.0001, 0.0005, 0.001} {
		h := Encode(lat+d, lng+d, 6)
		if CommonPrefix(base, h) < 4 {
			t.Errorf("nearby point %q shares only %d chars with %q", h, CommonPrefix(base, h), base)
		}
	}
}

// ⚠️ The converse does not hold, and this test documents it. Two points metres
// apart across a cell boundary can share no prefix at all.
func TestBoundaryProblemIsReal(t *testing.T) {
	a := Encode(51.5074, -0.00001, 7)
	b := Encode(51.5074, 0.00001, 7)

	distance := Haversine(51.5074, -0.00001, 51.5074, 0.00001)
	if distance > 10 {
		t.Fatalf("test setup wrong: points are %.1f m apart", distance)
	}
	if prefix := CommonPrefix(a, b); prefix > 2 {
		t.Errorf("expected these boundary-straddling points to share few characters, got %d", prefix)
	}
}

// Distant points must not share a long prefix.
func TestDistantPointsDoNotSharePrefixes(t *testing.T) {
	london := Encode(51.5074, -0.1278, 8)
	sydney := Encode(-33.8688, 151.2093, 8)

	if CommonPrefix(london, sydney) > 1 {
		t.Errorf("London %q and Sydney %q share %d characters", london, sydney, CommonPrefix(london, sydney))
	}
}

func TestNeighborsCount(t *testing.T) {
	n, err := Neighbors(Encode(51.5074, -0.1278, 6))
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 8 {
		t.Errorf("got %d neighbours, want 8", len(n))
	}
}

func TestNeighborsAreDistinctAndExcludeCenter(t *testing.T) {
	center := Encode(51.5074, -0.1278, 6)
	neighbors, _ := Neighbors(center)

	seen := map[string]bool{}
	for _, n := range neighbors {
		if n == center {
			t.Error("the centre cell should not be among its own neighbours")
		}
		if seen[n] {
			t.Errorf("duplicate neighbour %q", n)
		}
		seen[n] = true
	}
}

// Every neighbour must genuinely be adjacent, i.e. roughly one cell away.
func TestNeighborsAreAdjacent(t *testing.T) {
	center := Encode(51.5074, -0.1278, 6)
	cBox, _ := Decode(center)
	cLat, cLng := cBox.Center()
	cellHeight := cBox.MaxLat - cBox.MinLat
	cellWidth := cBox.MaxLng - cBox.MinLng

	neighbors, _ := Neighbors(center)
	for _, n := range neighbors {
		box, _ := Decode(n)
		lat, lng := box.Center()
		if math.Abs(lat-cLat) > cellHeight*1.6 || math.Abs(lng-cLng) > cellWidth*1.6 {
			t.Errorf("neighbour %q at %.4f,%.4f is not adjacent to %.4f,%.4f", n, lat, lng, cLat, cLng)
		}
	}
}

// ⭐ The fix for the boundary problem: searching the 3x3 block must find a
// neighbour that a prefix-only search would miss.
func TestSearchAreaCatchesBoundaryNeighbours(t *testing.T) {
	const lat, lng = 51.5074, -0.00001
	area, err := SearchArea(lat, lng, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(area) != 9 {
		t.Fatalf("search area has %d cells, want 9", len(area))
	}

	// A point 2 m away across the meridian must fall inside one of the 9 cells.
	target := Encode(51.5074, 0.00001, 7)
	for _, cell := range area {
		if cell == target {
			return
		}
	}
	t.Errorf("target cell %q not covered by the search area %v", target, area)
}

func TestWrappingAtAntimeridian(t *testing.T) {
	// Near +180: neighbours must wrap rather than produce invalid coordinates.
	neighbors, err := Neighbors(Encode(0, 179.99, 5))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range neighbors {
		box, err := Decode(n)
		if err != nil {
			t.Fatalf("invalid neighbour %q: %v", n, err)
		}
		if box.MinLng < -180.01 || box.MaxLng > 180.01 {
			t.Errorf("neighbour %q has out-of-range longitude", n)
		}
	}
}

func TestPolesAreClamped(t *testing.T) {
	for _, lat := range []float64{89.99, -89.99} {
		neighbors, err := Neighbors(Encode(lat, 0, 5))
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range neighbors {
			box, err := Decode(n)
			if err != nil {
				t.Fatal(err)
			}
			if box.MinLat < -90.01 || box.MaxLat > 90.01 {
				t.Errorf("neighbour %q has out-of-range latitude", n)
			}
		}
	}
}

func TestInvalidCharactersRejected(t *testing.T) {
	// a, i, l and o are excluded from the alphabet.
	for _, h := range []string{"aaaaa", "iiiii", "lllll", "ooooo", "u4pr!"} {
		if _, err := Decode(h); err == nil {
			t.Errorf("Decode(%q) should have failed", h)
		}
	}
}

func TestEmptyHashIsTheWholeWorld(t *testing.T) {
	box, err := Decode("")
	if err != nil {
		t.Fatal(err)
	}
	if box.MinLat != -90 || box.MaxLat != 90 || box.MinLng != -180 || box.MaxLng != 180 {
		t.Errorf("empty hash = %+v, want the whole world", box)
	}
}

func TestPrecisionIsClamped(t *testing.T) {
	if got := Encode(51.5, -0.1, 0); len(got) != 1 {
		t.Errorf("precision 0 produced %q, want 1 character", got)
	}
}

// Cell sizes should match the published table.
func TestCellSizesMatchPublishedTable(t *testing.T) {
	cases := []struct {
		precision int
		maxMetres float64
	}{
		{5, 6000}, {6, 1500}, {7, 200}, {8, 50},
	}
	for _, c := range cases {
		box, _ := Decode(Encode(51.5074, -0.1278, c.precision))
		width := Haversine(51.5074, box.MinLng, 51.5074, box.MaxLng)
		if width > c.maxMetres {
			t.Errorf("precision %d cell is %.0f m wide, want under %.0f", c.precision, width, c.maxMetres)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Encode(51.5074, -0.1278, 9)
	}
}

func BenchmarkNeighbors(b *testing.B) {
	h := Encode(51.5074, -0.1278, 7)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Neighbors(h)
	}
}
