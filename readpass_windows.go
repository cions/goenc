// Copyright (c) 2020 cions
// Licensed under the MIT License. See LICENSE for details

// +build windows

package main

import (
	"os"

	"golang.org/x/term"
)

type windowsTTY struct {
	conin, conout *os.File
}

func newTTY() (tty, error) {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	conout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
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
	return term.MakeRaw(int(t.conin.Fd()))
}

func (t *windowsTTY) Restore(oldState *term.State) error {
	return term.Restore(int(t.conin.Fd()), oldState)
}
