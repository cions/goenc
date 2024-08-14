// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/cions/goenc"
	"github.com/cions/goenc/prompt"
)

func getVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "(devel)"
}

func getPassword(maxRetries uint8) ([]byte, error) {
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		return []byte(value), nil
	}

	terminal, err := prompt.NewTerminal()
	if err != nil {
		return nil, err
	}
	defer terminal.Close()

	tries := uint8(1)
	for {
		password, err := terminal.ReadPassword(context.Background(), "Password: ")
		if err != nil {
			return nil, err
		}
		confirmPassword, err := terminal.ReadPassword(context.Background(), "Confirm Password: ")
		if err != nil {
			return nil, err
		}
		if bytes.Equal(password, confirmPassword) {
			return password, nil
		} else if tries < maxRetries {
			fmt.Fprintln(terminal, "goenc: error: passwords does not match. try again.")
			tries++
		} else {
			return nil, errors.New("passwords does not match")
		}
	}
}

func encrypt(opts *options) error {
	var r io.Reader = os.Stdin
	if opts.Input != "-" {
		fh, err := os.Open(opts.Input)
		if err != nil {
			return err
		}
		defer fh.Close()
		r = fh
	}

	password, err := getPassword(opts.Retries)
	if err != nil {
		return err
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	ciphertext, err := goenc.Encrypt(password, plaintext, &goenc.Options{
		Time:    opts.Time,
		Memory:  opts.Memory,
		Threads: opts.Threads,
	})
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		fh, err := os.OpenFile(opts.Output, flags, 0o666)
		if err != nil {
			return err
		}
		defer fh.Close()
		w = fh
	}

	if _, err := w.Write(ciphertext); err != nil {
		return err
	}

	return nil
}

func tryDecrypt(input []byte, maxRetries uint8) ([]byte, error) {
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		password := []byte(value)
		return goenc.Decrypt(password, input)
	}

	terminal, err := prompt.NewTerminal()
	if err != nil {
		return nil, err
	}
	defer terminal.Close()

	tries := uint8(1)
	for {
		password, err := terminal.ReadPassword(context.Background(), "Password: ")
		if err != nil {
			return nil, err
		}
		plaintext, err := goenc.Decrypt(password, input)
		if errors.Is(err, goenc.ErrInvalidTag) && tries < maxRetries {
			fmt.Fprintln(terminal, "goenc: error: incorrect password. try again.")
			tries++
			continue
		} else if err != nil {
			return nil, err
		}
		return plaintext, nil
	}
}

func decrypt(opts *options) error {
	var r io.Reader = os.Stdin
	if opts.Input != "-" {
		fh, err := os.Open(opts.Input)
		if err != nil {
			return err
		}
		defer fh.Close()
		r = fh
	}

	input, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	plaintext, err := tryDecrypt(input, opts.Retries)
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		fh, err := os.OpenFile(opts.Output, flags, 0o666)
		if err != nil {
			return err
		}
		defer fh.Close()
		w = fh
	}

	if _, err := w.Write(plaintext); err != nil {
		return err
	}

	return nil
}

func runCommand(args []string) error {
	opts := &options{
		Operation: opEncrypt,
		NoClobber: false,
		Time:      8,
		Memory:    256 << 10, // 256 MiB
		Threads:   4,
		Retries:   3,
		Input:     "-",
		Output:    "-",
	}

	err := opts.ParseArgs(args)
	if err != nil {
		return err
	}

	switch opts.Operation {
	case opHelp:
		fmt.Println(helpMessage)
	case opVersion:
		fmt.Printf("goenc %s (%s/%s)\n", getVersion(), runtime.GOOS, runtime.GOARCH)
	case opEncrypt:
		return encrypt(opts)
	case opDecrypt:
		return decrypt(opts)
	default:
		panic("goenc: invalid operation")
	}

	return nil
}

func main() {
	if err := runCommand(os.Args[1:]); err != nil {
		var se prompt.SignalError
		if errors.As(err, &se) {
			os.Exit(128 + se.Signal())
		}
		fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
		if errors.Is(err, goenc.ErrInvalidTag) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}
