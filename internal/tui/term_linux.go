//go:build linux

package tui

import (
	"syscall"
	"unsafe"
)

// rawMode puts fd into raw mode and returns a restore func. Linux uses
// TCGETS/TCSETS for the termios get/set ioctls.
func rawMode(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TCSETS, &old) }, nil
}

func ioctl(fd, req uintptr, t *syscall.Termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

// winsize mirrors struct winsize for the TIOCGWINSZ ioctl.
type winsize struct {
	rows, cols, xpix, ypix uint16
}

// termSize returns the terminal (cols, rows) for fd, or (0,0) if unavailable.
func termSize(fd uintptr) (int, int) {
	var ws winsize
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if e != 0 {
		return 0, 0
	}
	return int(ws.cols), int(ws.rows)
}
