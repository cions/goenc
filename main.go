// Copyright (c) 2020-2022 cions
// Licensed under the MIT License. See LICENSE for details

package goenc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/cions/goenc/prompt"
)

// ErrInvalidTag if tag verification failed
var ErrInvalidTag = errors.New("tag verification failed (password is wrong or data is corrupted)")

func getVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "(devel)"
}

func encrypt(opts *options) error {
	var password []byte
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		password = []byte(value)
	} else {
		terminal, err := prompt.NewTerminal()
		if err != nil {
			return err
		}
		defer terminal.Close()

		var tries uint8 = 1
		for {
			password, err = terminal.ReadPassword(context.Background(), "Password: ")
			if err != nil {
				return err
			}
			confirmPassword, err := terminal.ReadPassword(context.Background(), "Confirm Password: ")
			if err != nil {
				return err
			}
			if bytes.Equal(password, confirmPassword) {
				break
			} else if tries < opts.Retries {
				fmt.Fprintln(terminal, "goenc: error: passwords does not match. try again.")
				tries++
				continue
			} else {
				return errors.New("passwords does not match")
			}
		}
	}

	var r io.Reader = os.Stdin
	if opts.Input != "-" {
		fh, err := os.Open(opts.Input)
		if err != nil {
			return err
		}
		defer fh.Close()
		r = fh
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	ciphertext, err := encryptV1(password, plaintext, opts)
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		fh, err := os.OpenFile(opts.Output, flags, 0o644)
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
	if len(input) == 0 {
		return io.ErrUnexpectedEOF
	}

	var decryptor func([]byte, []byte, *options) ([]byte, error)
	switch input[0] {
	case 0x01:
		if len(input) < minSizeV1 {
			return io.ErrUnexpectedEOF
		}
		decryptor = decryptV1
	default:
		return errors.New("invalid file format")
	}

	var plaintext []byte
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		password := []byte(value)
		plaintext, err = decryptor(password, input, opts)
		if err != nil {
			return err
		}
	} else {
		terminal, err := prompt.NewTerminal()
		if err != nil {
			return err
		}
		defer terminal.Close()

		var tries uint8 = 1
		for {
			password, err := terminal.ReadPassword(context.Background(), "Password: ")
			if err != nil {
				return err
			}
			plaintext, err = decryptor(password, input, opts)
			if errors.Is(err, ErrInvalidTag) && tries < opts.Retries {
				fmt.Fprintln(terminal, "goenc: error: incorrect password. try again.")
				tries++
				continue
			} else if err != nil {
				return err
			} else {
				break
			}
		}
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		fh, err := os.OpenFile(opts.Output, flags, 0o644)
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

// Main runs the command
func Main(args []string) error {
	opts, err := parseArgs(args[1:])
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
