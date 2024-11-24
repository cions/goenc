// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

//go:build windows

package prompt

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

const (
	clreos = "\x1b[J" // Clear to end of screen
	sc     = "\x1b[s" // Save cursor
	rc     = "\x1b[u" // Restore cursor
	ewrap  = ""       // Enable wraparound mode (DECAWM) and reverse wrap mode (REVERSEWRAP)
	ebp    = ""       // Enable Bracketed Paste Mode
	dbp    = ""       // Disable Bracketed Paste Mode
)

// Terminal represents a terminal.
type Terminal struct {
	conin, conout   *os.File
	inMode, outMode uint32
}

// NewTerminal opens a terminal.
func NewTerminal() (*Terminal, error) {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	conout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.Join(err, conin.Close())
	}
	return &Terminal{conin: conin, conout: conout}, nil
}

// Read reads up to len(p) bytes from the terminal.
func (t *Terminal) Read(p []byte) (int, error) {
	return t.conin.Read(p)
}

// ContextReader returns an io.ReadCloser that cancels the Read operation when
// context's Done channel is closed.
func (t *Terminal) ContextReader(ctx context.Context) (io.ReadCloser, error) {
	go func() {
		<-ctx.Done()
		windows.CancelIoEx(windows.Handle(t.conin.Fd()), nil)
	}()
	return &contextReader{ctx, t.conin}, nil
}

// Write writes len(p) bytes to the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return t.conout.Write(p)
}

// Close closes the terminal.
func (t *Terminal) Close() error {
	err1 := t.conin.Close()
	err2 := t.conout.Close()
	return errors.Join(err1, err2)
}

// MakeRaw puts the terminal into raw mode.
func (t *Terminal) MakeRaw() error {
	if err := windows.GetConsoleMode(windows.Handle(t.conin.Fd()), &t.inMode); err != nil {
		return &os.SyscallError{Syscall: "GetConsoleMode", Err: err}
	}
	if err := windows.GetConsoleMode(windows.Handle(t.conout.Fd()), &t.outMode); err != nil {
		return &os.SyscallError{Syscall: "GetConsoleMode", Err: err}
	}

	inMode := t.inMode
	inMode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), inMode); err != nil {
		return &os.SyscallError{Syscall: "SetConsoleMode", Err: err}
	}

	outMode := t.outMode
	outMode |= windows.ENABLE_PROCESSED_OUTPUT
	outMode |= windows.ENABLE_WRAP_AT_EOL_OUTPUT
	outMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	outMode |= windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), outMode); err != nil {
		return &os.SyscallError{Syscall: "SetConsoleMode", Err: err}
	}

	return nil
}

// Restore restores the terminal to the state prior to calling MakeRaw.
func (t *Terminal) Restore() error {
	err1 := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), t.inMode)
	if err1 != nil {
		err1 = &os.SyscallError{Syscall: "SetConsoleMode", Err: err1}
	}
	err2 := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), t.outMode)
	if err2 != nil {
		err2 = &os.SyscallError{Syscall: "SetConsoleMode", Err: err2}
	}
	return errors.Join(err1, err2)
}

// GetSize returns the visible dimensions of the terminal.
func (t *Terminal) GetSize() (width, height int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(t.conout.Fd()), &info); err != nil {
		return 0, 0, &os.SyscallError{Syscall: "GetConsoleScreenBufferInfo", Err: err}
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err2 := context.Cause(r.ctx); err2 != nil {
		return n, err2
	}
	return n, err
}

func (*contextReader) Close() error {
	return nil
}
