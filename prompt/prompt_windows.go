// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

//go:build windows
// +build windows

package prompt

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

type windowsTTY struct {
	conin, conout   *os.File
	inMode, outMode uint32
}

func newTTY() (tty, error) {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	conout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		conin.Close()
		return nil, err
	}

	return &windowsTTY{conin: conin, conout: conout}, nil
}

func (t *windowsTTY) Read(b []byte) (int, error) {
	return t.conin.Read(b)
}

func (t *windowsTTY) Write(b []byte) (int, error) {
	return t.conout.Write(b)
}

func (t *windowsTTY) Close() error {
	err1 := t.conin.Close()
	err2 := t.conout.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (t *windowsTTY) MakeRaw() (*term.State, error) {
	if err := windows.GetConsoleMode(windows.Handle(t.conin.Fd()), &t.inMode); err != nil {
		return nil, err
	}

	var mode uint32 = windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), mode); err != nil {
		return nil, err
	}

	if err := windows.GetConsoleMode(windows.Handle(t.conout.Fd()), &t.outMode); err != nil {
		return nil, err
	}

	mode = windows.ENABLE_PROCESSED_OUTPUT
	mode |= windows.ENABLE_WRAP_AT_EOL_OUTPUT
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	mode |= windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), mode); err != nil {
		return nil, err
	}

	return nil, nil
}

func (t *windowsTTY) Restore(oldState *term.State) error {
	if err := windows.SetConsoleMode(windows.Handle(t.conin.Fd()), t.inMode); err != nil {
		return err
	}
	if err := windows.SetConsoleMode(windows.Handle(t.conout.Fd()), t.outMode); err != nil {
		return err
	}
	return nil
}
