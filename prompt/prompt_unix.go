// Copyright (c) 2020-2026 cions <cions@tuta.com>
// SPDX-License-Identifier: MIT

//go:build !windows

package prompt

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	clreos = "\x1b[J"            // Clear to end of screen
	sc     = "\x1b[s"            // Save cursor
	rc     = "\x1b[u"            // Restore cursor
	ewrap  = "\x1b[?7h\x1b[?45h" // Enable wraparound mode (DECAWM) and reverse wrap mode (REVERSEWRAP)
	ebp    = "\x1b[?2004h"       // Enable Bracketed Paste Mode
	dbp    = "\x1b[?2004l"       // Disable Bracketed Paste Mode
)

// Terminal represents a terminal.
type Terminal struct {
	fd       int
	oldState *term.State
}

// NewTerminal opens a terminal.
func NewTerminal() (*Terminal, error) {
	devices := []string{
		"/proc/self/fd/0",
		"/dev/fd/0",
		"/dev/stdin",
		"/proc/self/fd/1",
		"/dev/fd/1",
		"/dev/stdout",
		"/proc/self/fd/2",
		"/dev/fd/2",
		"/dev/stderr",
		"/dev/tty",
		"/dev/console",
	}
	for _, device := range devices {
		fd, err := unix.Open(device, unix.O_RDWR|unix.O_NOCTTY, 0)
		if err != nil {
			continue
		}
		if term.IsTerminal(fd) {
			return &Terminal{fd: fd}, nil
		}
		unix.Close(fd)
	}
	return nil, errors.New("failed to open a terminal")
}

// Read reads up to len(p) bytes from the terminal.
func (t *Terminal) Read(p []byte) (int, error) {
	n, err := ignoringEINTRIO(unix.Read, t.fd, p)
	if err != nil {
		return n, &os.PathError{Op: "read", Path: "/dev/tty", Err: err}
	}
	return n, nil
}

// ContextReader returns an io.ReadCloser that cancels the Read operation when
// context's Done channel is closed.
func (t *Terminal) ContextReader(ctx context.Context) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)

	var pipe [2]int
	err := ignoringEINTR(func() error { return unix.Pipe(pipe[:]) })
	if err != nil {
		cancel()
		return nil, &os.SyscallError{Syscall: "pipe", Err: err}
	}

	go func() {
		<-ctx.Done()
		unix.Close(pipe[1])
	}()

	return &contextReader{
		ctx:    ctx,
		t:      t,
		pfd:    pipe[0],
		cancel: cancel,
	}, nil
}

// Write writes len(p) bytes to the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	nn := 0
	for nn < len(p) {
		n, err := ignoringEINTRIO(unix.Write, t.fd, p[nn:])
		if err != nil {
			return nn, &os.PathError{Op: "write", Path: "/dev/tty", Err: err}
		}
		if n == 0 {
			return nn, io.ErrShortWrite
		}
		nn += n
	}
	return nn, nil
}

// Close closes the terminal.
func (t *Terminal) Close() error {
	if err := unix.Close(t.fd); err != nil {
		return &os.PathError{Op: "close", Path: "/dev/tty", Err: err}
	}
	return nil
}

// MakeRaw puts the terminal into raw mode.
func (t *Terminal) MakeRaw() error {
	if t.oldState != nil {
		return errors.New("terminal is already in raw mode")
	}
	oldState, err := term.MakeRaw(t.fd)
	if err != nil {
		return &os.SyscallError{Syscall: "ioctl", Err: err}
	}
	t.oldState = oldState
	return nil
}

// Restore restores the terminal to the state prior to calling MakeRaw.
func (t *Terminal) Restore() error {
	if t.oldState == nil {
		return errors.New("terminal is not in raw mode")
	}
	if err := term.Restore(t.fd, t.oldState); err != nil {
		return &os.SyscallError{Syscall: "ioctl", Err: err}
	}
	t.oldState = nil
	return nil
}

// GetSize returns the visible dimensions of the terminal.
func (t *Terminal) GetSize() (width, height int, err error) {
	width, height, err = term.GetSize(t.fd)
	if err != nil {
		err = &os.SyscallError{Syscall: "ioctl", Err: err}
	}
	return
}

type contextReader struct {
	ctx    context.Context
	t      *Terminal
	pfd    int
	cancel context.CancelFunc
}

func (r *contextReader) Read(p []byte) (int, error) {
	fds := []unix.PollFd{
		{Fd: int32(r.t.fd), Events: unix.POLLIN},
		{Fd: int32(r.pfd), Events: unix.POLLIN},
	}
	err := ignoringEINTR(func() error {
		_, err := unix.Poll(fds, -1)
		return err
	})
	if err != nil {
		return 0, &os.SyscallError{Syscall: "poll", Err: err}
	}
	if fds[1].Revents != 0 {
		return 0, context.Cause(r.ctx)
	}
	return r.t.Read(p)
}

func (r *contextReader) Close() error {
	if err := unix.Close(r.pfd); err != nil {
		return &os.SyscallError{Syscall: "close", Err: err}
	}
	r.cancel()
	return nil
}

func ignoringEINTR(f func() error) error {
	for {
		err := f()
		if err != unix.EINTR {
			return err
		}
	}
}

func ignoringEINTRIO(f func(fd int, p []byte) (int, error), fd int, p []byte) (int, error) {
	for {
		n, err := f(fd, p)
		if err != unix.EINTR {
			return n, err
		}
	}
}
