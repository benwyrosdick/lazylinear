package ui

import "testing"

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
