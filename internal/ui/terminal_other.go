//go:build !unix

package ui

import "os"

// detectKittyGraphics reports no graphics support on platforms without a Unix terminal
func detectKittyGraphics(tty *os.File) bool {
	return false
}

// cellPixelSize returns zeros because the cell size is unknown on this platform
func cellPixelSize(tty *os.File) (int, int) {
	return 0, 0
}
