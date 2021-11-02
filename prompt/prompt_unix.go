// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris
// +build aix darwin dragonfly freebsd illumos linux netbsd openbsd solaris

package prompt

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Terminal represents an local terminal.
type Terminal struct {
	fd       int
	oldState *term.State
}

// NewTerminal returns the Terminal.
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
	return nil, errors.New("failed to open the terminal")
}

// Read reads up to len(p) bytes from the terminal.
func (t *Terminal) Read(p []byte) (int, error) {
	return ignoringEINTRIO(unix.Read, t.fd, p)
}

// ReadContext reads up to len(p) bytes from the terminal. If the context expires
// before reading any data, ReadContext returns the context's error,
func (t *Terminal) ReadContext(ctx context.Context, p []byte) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var pipe [2]int
	err := ignoringEINTR(func() error {
		return unix.Pipe(pipe[:])
	})
	if err != nil {
		return 0, fmt.Errorf("pipe: %w", err)
	}
	go func() {
		<-ctx.Done()
		unix.Close(pipe[1])
	}()
	defer unix.Close(pipe[0])

	fds := []unix.PollFd{
		{Fd: int32(t.fd), Events: unix.POLLIN},
		{Fd: int32(pipe[0]), Events: unix.POLLIN},
	}
	err = ignoringEINTR(func() error {
		_, err := unix.Poll(fds, -1)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("poll: %w", err)
	}
	if fds[0].Revents&unix.POLLIN == 0 {
		return 0, ctx.Err()
	}
	return t.Read(p)
}

// Write writes len(p) bytes to the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
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
