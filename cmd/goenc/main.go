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
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/cions/go-options"
	"github.com/cions/goenc"
	"github.com/cions/goenc/prompt"
)

var NAME = "goenc"
var VERSION = "(devel)"
var USAGE = `Usage: $NAME [OPTIONS] [INPUT] [OUTPUT]

A simple file encryption tool

Options:
  -e, --encrypt         Encrypt
  -d, --decrypt         Decrypt
  -n, --no-clobber      Do not overwrite an existing file
  -t, --time=N          Argon2 time parameter (default: 8)
  -m, --memory=N[KMG]   Argon2 memory parameter (default: 256M)
  -p, --parallelism=N   Argon2 parallelism parameter (default: 4)
  -r, --retries=N       Maximum number of attempts to enter password
                        (default: 3)
  -h, --help            Show this help message and exit
      --version         Show version information and exit

Environment Variables:
  PASSWORD              Encryption password

Exit Status:
  0  Operation was successful
  1  Tag verification failed (password is wrong or data is corrupted)
  2  Invalid command line
  3  An error occurred
`

type Options struct {
	Encrypt   bool
	NoClobber bool
	Time      uint32
	Memory    uint32
	Threads   uint8
	Retries   uint8
	Input     string
	Output    string
}

func (opts *Options) Kind(name string) options.Kind {
	switch name {
	case "-e", "--encrypt":
		return options.Boolean
	case "-d", "--decrypt":
		return options.Boolean
	case "-n", "--no-clobber":
		return options.Boolean
	case "-t", "--time":
		return options.Required
	case "-m", "--memory":
		return options.Required
	case "-p", "--parallelism":
		return options.Required
	case "-r", "--retries":
		return options.Required
	case "-h", "--help":
		return options.Boolean
	case "--version":
		return options.Boolean
	default:
		return options.Unknown
	}
}

func (opts *Options) Option(name string, value string, hasValue bool) error {
	switch name {
	case "-e", "--encrypt":
		opts.Encrypt = true
	case "-d", "--decrypt":
		opts.Encrypt = false
	case "-n", "--no-clobber":
		opts.NoClobber = true
	case "-t", "--time":
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return err
		} else if n == 0 {
			return strconv.ErrRange
		}
		opts.Time = uint32(n)
	case "-m", "--memory":
		unit := uint64(1)
		width := 32
		switch {
		case strings.HasSuffix(value, "K"):
			value = strings.TrimSuffix(value, "K")
		case strings.HasSuffix(value, "M"):
			value = strings.TrimSuffix(value, "M")
			unit = 1 << 10
			width -= 10
		case strings.HasSuffix(value, "G"):
			value = strings.TrimSuffix(value, "G")
			unit = 1 << 20
			width -= 20
		}

		n, err := strconv.ParseUint(value, 10, width)
		if err != nil {
			return err
		} else if n == 0 {
			return strconv.ErrRange
		}
		opts.Memory = uint32(n * unit)
	case "-p", "--parallelism":
		n, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return err
		} else if n == 0 {
			return strconv.ErrRange
		}
		opts.Threads = uint8(n)
	case "-r", "--retries":
		n, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return err
		} else if n == 0 {
			return strconv.ErrRange
		}
		opts.Retries = uint8(n)
	case "-h", "--help":
		return options.ErrHelp
	case "--version":
		return options.ErrVersion
	default:
		return options.ErrUnknown
	}
	return nil
}

func (opts *Options) Arg(index int, value string, afterDDash bool) error {
	switch index {
	case 0:
		opts.Input = value
	case 1:
		opts.Output = value
	default:
		return options.Errorf("too many arguments")
	}
	return nil
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
			fmt.Fprintf(terminal, "%v: error: passwords does not match. try again.\n", NAME)
			tries++
		} else {
			return nil, errors.New("passwords does not match")
		}
	}
}

func encrypt(opts *Options) error {
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
			fmt.Fprintf(terminal, "%v: error: incorrect password. try again.\n", NAME)
			tries++
			continue
		} else if err != nil {
			return nil, err
		}
		return plaintext, nil
	}
}

func decrypt(opts *Options) error {
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

func run(args []string) error {
	opts := &Options{
		Encrypt:   true,
		NoClobber: false,
		Time:      8,
		Memory:    256 << 10, // 256 MiB
		Threads:   4,
		Retries:   3,
		Input:     "-",
		Output:    "-",
	}

	_, err := options.Parse(opts, args)
	if errors.Is(err, options.ErrHelp) {
		usage := strings.ReplaceAll(USAGE, "$NAME", NAME)
		fmt.Print(usage)
		return nil
	} else if errors.Is(err, options.ErrVersion) {
		version := VERSION
		if bi, ok := debug.ReadBuildInfo(); ok {
			version = bi.Main.Version
		}
		fmt.Printf("%v %v\n", NAME, version)
		return nil
	} else if err != nil {
		return err
	}

	if opts.Encrypt {
		return encrypt(opts)
	} else {
		return decrypt(opts)
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var sigerr prompt.SignalError
		if errors.As(err, &sigerr) {
			os.Exit(128 + sigerr.Signal())
		}

		fmt.Fprintf(os.Stderr, "%v: error: %v\n", NAME, err)
		switch {
		case errors.Is(err, goenc.ErrInvalidTag):
			os.Exit(1)
		case errors.Is(err, options.ErrCmdline):
			os.Exit(2)
		default:
			os.Exit(3)
		}
	}
}
