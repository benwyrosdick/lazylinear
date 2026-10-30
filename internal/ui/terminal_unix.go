//go:build unix

package ui

import (
	"bytes"
	"os"
	"regexp"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// deviceAttributesPattern matches the terminal's reply to a primary device attributes request
var deviceAttributesPattern = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)

// detectKittyGraphics asks the terminal whether it implements the kitty graphics protocol.
// It must run before gocui takes over the terminal.
func detectKittyGraphics(tty *os.File) bool {
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return false
	}
	defer term.Restore(fd, state)

	// Query with a 1x1 image, then request device attributes, which every terminal
	// answers. A terminal with graphics support replies to the query first.
	if _, err := tty.WriteString("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c"); err != nil {
		return false
	}

	var reply []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(time.Second)
	for !deviceAttributesPattern.Match(reply) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			break
		}
		n, err = unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			break
		}
		reply = append(reply, buf[:n]...)
	}
	return bytes.Contains(reply, []byte("\x1b_Gi=31;OK"))
}

// cellPixelSize returns the width and height of a terminal cell in pixels, or zeros if unknown
func cellPixelSize(tty *os.File) (int, int) {
	ws, err := unix.IoctlGetWinsize(int(tty.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}
