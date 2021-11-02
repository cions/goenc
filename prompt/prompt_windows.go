// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

//go:build windows
// +build windows

package prompt

import (
	"context"
	"os"

	"golang.org/x/sys/windows"
)

// Terminal represents an local terminal.
type Terminal struct {
	conin, conout   *os.File
	inMode, outMode uint32
}

// NewTerminal returns the Terminal.
func NewTerminal() (*Terminal, error) {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	conout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		conin.Close()
		return nil, err
	}

	return &Terminal{conin: conin, conout: conout}, nil
}

// Read reads up to len(p) bytes from the terminal.
func (t *Terminal) Read(p []byte) (int, error) {
	return t.conin.Read(p)
}

// ReadContext reads up to len(p) bytes from the terminal. If the context expires
// before reading any data, ReadContext returns the context's error,
func (t *Terminal) ReadContext(ctx context.Context, p []byte) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		windows.CancelIoEx(windows.Handle(t.conin.Fd()), nil)
	}()
	return t.conin.Read(p)
}

// Write writes len(p) bytes to the terminal.
func (t *Terminal) Write(p []byte) (int, error) {
	return t.conout.Write(p)
}

// Close closes the terminal.
func (t *Terminal) Close() error {
	err1 := t.conin.Close()
	err2 := t.conout.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// MakeRaw puts the terminal into raw mode.
func (t *Terminal) MakeRaw() error {
	if err := windows.GetConsoleMode(windows.Handle(t.conin.Fd()), &t.inMode); err != nil {
		return err
	}

	var inMode uint32
	inMode |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), inMode); err != nil {
		return err
	}

	if err := windows.GetConsoleMode(windows.Handle(t.conout.Fd()), &t.outMode); err != nil {
		return err
	}

	var outMode uint32
	outMode |= windows.ENABLE_PROCESSED_OUTPUT
	outMode |= windows.ENABLE_WRAP_AT_EOL_OUTPUT
	outMode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	outMode |= windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), outMode); err != nil {
		return err
	}

	return nil
}

// Restore restores the terminal to the state prior to calling MakeRaw.
func (t *Terminal) Restore() error {
	if err := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), t.inMode); err != nil {
		return err
	}
	if err := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), t.outMode); err != nil {
		return err
	}
	return nil
}
