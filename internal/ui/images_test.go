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
