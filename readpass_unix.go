// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

// +build !windows

package main

import (
	"errors"
	"os"

	"golang.org/x/term"
)

type unixTTY struct {
	tty         *os.File
	needToClose bool
}

func newTTY() (tty, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return &unixTTY{tty: os.Stdin, needToClose: false}, nil
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return &unixTTY{tty: os.Stdout, needToClose: false}, nil
	}
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return &unixTTY{tty: os.Stderr, needToClose: false}, nil
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		return &unixTTY{tty: tty, needToClose: true}, nil
	}
	if tty, err := os.OpenFile("/dev/console", os.O_RDWR, 0); err == nil {
		return &unixTTY{tty: tty, needToClose: true}, nil
	}
	return nil, errors.New("failed to open the terminal")
}

func (t *unixTTY) Read(b []byte) (int, error) {
	return t.tty.Read(b)
}

func (t *unixTTY) Write(b []byte) (int, error) {
	return t.tty.Write(b)
}

func (t *unixTTY) Close() error {
	if t.needToClose {
		return t.tty.Close()
	}
	return nil
}

func (t *unixTTY) MakeRaw() (*term.State, error) {
	return term.MakeRaw(int(t.tty.Fd()))
}

func (t *unixTTY) Restore(oldState *term.State) error {
	return term.Restore(int(t.tty.Fd()), oldState)
}
