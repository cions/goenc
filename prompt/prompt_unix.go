// Copyright (c) 2020-2023 cions
// Licensed under the MIT License. See LICENSE for details.

//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

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

type contextReader struct {
	t   *Terminal
	pfd int
	ctx context.Context
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
		return 0, fmt.Errorf("poll: %w", err)
	}
	if fds[1].Revents != 0 {
		return 0, context.Cause(r.ctx)
	}
	return r.t.Read(p)
}

func (r *contextReader) Close() error {
	return unix.Close(r.pfd)
}

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
	return ignoringEINTRIO(unix.Read, t.fd, p)
}

// ContextReader returns an io.ReadCloser that cancels the Read operation when
// context's Done channel is closed.
func (t *Terminal) ContextReader(ctx context.Context) (io.ReadCloser, error) {
	var pipe [2]int
	err := ignoringEINTR(func() error {
		return unix.Pipe(pipe[:])
	})
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	go func() {
		<-ctx.Done()
		unix.Close(pipe[1])
	}()

	return &contextReader{
		t:   t,
		pfd: pipe[0],
		ctx: ctx,
	}, nil
}

// Write writes len(p) bytes to the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return ignoringEINTRIO(unix.Write, t.fd, p)
}

// Close closes the terminal.
func (t *Terminal) Close() error {
	return unix.Close(t.fd)
}

// MakeRaw puts the terminal into raw mode.
func (t *Terminal) MakeRaw() error {
	oldState, err := term.MakeRaw(t.fd)
	if err != nil {
		return err
	}
	t.oldState = oldState
	return nil
}

// Restore restores the terminal to the state prior to calling MakeRaw.
func (t *Terminal) Restore() error {
	return term.Restore(t.fd, t.oldState)
}

// GetSize returns the visible dimensions of the given terminal.
func (t *Terminal) GetSize() (width, height int, err error) {
	return term.GetSize(t.fd)
}
