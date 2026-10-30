package ui

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestImageSize(t *testing.T) {
	tests := []struct {
		name               string
		width, height      int
		maxCols, maxRows   int
		wantCols, wantRows int
	}{
		{"natural size fits", 100, 40, 80, 40, 10, 2},
		{"shrunk to width", 2000, 1000, 50, 40, 50, 13},
		{"shrunk to height", 400, 4000, 80, 10, 2, 10},
	}
	for _, tt := range tests {
		cols, rows := imageSize(tt.width, tt.height, 10, 20, tt.maxCols, tt.maxRows)
		if cols != tt.wantCols || rows != tt.wantRows {
			t.Errorf("%s: got %dx%d, want %dx%d", tt.name, cols, rows, tt.wantCols, tt.wantRows)
		}
	}
}

func TestWrappedRows(t *testing.T) {
	tests := []struct {
		line  string
		width int
		want  int
	}{
		{"", 10, 1},
		{"short", 10, 1},
		{"exactly 10", 10, 2},
		{"twenty-one characters", 10, 3},
	}
	for _, tt := range tests {
		if got := wrappedRows(tt.line, tt.width); got != tt.want {
			t.Errorf("wrappedRows(%q, %d) = %d, want %d", tt.line, tt.width, got, tt.want)
		}
	}
}

func TestCutOut(t *testing.T) {
	img := &inlineImage{width: 100, height: 50}
	p := imagePlacement{image: img, x: 10, y: 10, cols: 10, rows: 5, cropW: 100, cropH: 50}

	if got := cutOut([]imagePlacement{p}, 30, 30, 5, 5); len(got) != 1 || got[0] != p {
		t.Errorf("rectangle away from the image changed it: %+v", got)
	}
	if got := cutOut([]imagePlacement{p}, 0, 0, 40, 40); len(got) != 0 {
		t.Errorf("rectangle covering the image left %d pieces", len(got))
	}

	// Covering the right half of rows 1-2 leaves a top band, bottom band and left piece
	got := cutOut([]imagePlacement{p}, 15, 11, 10, 2)
	want := []imagePlacement{
		{image: img, x: 10, y: 10, cols: 10, rows: 1, cropW: 100, cropH: 10},
		{image: img, x: 10, y: 13, cols: 10, rows: 2, cropY: 30, cropW: 100, cropH: 20},
		{image: img, x: 10, y: 11, cols: 5, rows: 2, cropY: 10, cropW: 50, cropH: 20},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pieces, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("piece %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// pngWithPHYs encodes a 1x1 PNG, adding a pHYs chunk with the given pixels per meter when nonzero
func pngWithPHYs(t *testing.T, pixelsPerMeter uint32) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if pixelsPerMeter == 0 {
		return data
	}

	body := binary.BigEndian.AppendUint32(nil, pixelsPerMeter)
	body = binary.BigEndian.AppendUint32(body, pixelsPerMeter)
	body = append(body, 1)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(chunk, "pHYs"...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	// The signature and IHDR chunk take the first 33 bytes; pHYs must come before IDAT
	return append(append(append([]byte{}, data[:33]...), chunk...), data[33:]...)
}

func TestPNGPixelScale(t *testing.T) {
	tests := []struct {
		name           string
		pixelsPerMeter uint32
		want           float64
	}{
		{"no pHYs", 0, 1},
		{"72 DPI", 2835, 1},
		{"144 DPI retina", 5669, 2},
		{"216 DPI", 8504, 3},
		{"300 DPI print", 11811, 1},
	}
	for _, tt := range tests {
		data := pngWithPHYs(t, tt.pixelsPerMeter)
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("%s: test image is invalid: %v", tt.name, err)
		}
		if got := pngPixelScale(data); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestImageCells(t *testing.T) {
	screenshot := &inlineImage{width: 924, height: 480, scale: 1}
	retina := &inlineImage{width: 1848, height: 960, scale: 2}
	tests := []struct {
		name               string
		img                *inlineImage
		cellW, cellH       int
		imageScale         float64
		wantCols, wantRows int
	}{
		{"1x terminal", screenshot, 9, 20, 0, 103, 24},
		{"2x terminal doubles the image", screenshot, 14, 32, 0, 132, 30},
		{"retina image on 2x terminal", retina, 14, 32, 0, 132, 30},
		{"retina image on 1x terminal", retina, 9, 20, 0, 103, 24},
		{"configured scale", screenshot, 14, 32, 1, 66, 15},
	}
	for _, tt := range tests {
		ui := &UI{cellWidth: tt.cellW, cellHeight: tt.cellH, imageScale: tt.imageScale}
		cols, rows := ui.imageCells(tt.img, 500, 500)
		if cols != tt.wantCols || rows != tt.wantRows {
			t.Errorf("%s: got %dx%d, want %dx%d", tt.name, cols, rows, tt.wantCols, tt.wantRows)
		}
	}
}
