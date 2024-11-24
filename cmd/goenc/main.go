// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

package main

import (
	"context"
	"crypto/subtle"
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
      --password-from=FILE
                        Read password from FILE
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
	Encrypt      bool
	NoClobber    bool
	Time         uint32
	Memory       uint32
	Threads      uint8
	Retries      uint8
	PasswordFrom string
	Input        string
	Output       string
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
	case "--password-from":
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
	case "--password-from":
		opts.PasswordFrom = value
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

func getPassword(opts *Options) (password []byte, err error) {
	if opts.PasswordFrom != "" {
		if opts.PasswordFrom == "-" {
			return io.ReadAll(os.Stdin)
		} else {
			return os.ReadFile(opts.PasswordFrom)
		}
	}
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		return []byte(value), nil
	}

	terminal, err2 := prompt.NewTerminal()
	if err2 != nil {
		return nil, err2
	}
	defer func() {
		err = errors.Join(err, terminal.Close())
	}()

	tries := uint8(1)
	for {
		password, err2 := terminal.ReadPassword(context.Background(), "Password: ")
		if err2 != nil {
			return nil, fmt.Errorf("ReadPassword: %w", err2)
		}
		confirmPassword, err2 := terminal.ReadPassword(context.Background(), "Confirm Password: ")
		if err2 != nil {
			return nil, fmt.Errorf("ReadPassword: %w", err2)
		}
		if subtle.ConstantTimeCompare(password, confirmPassword) != 0 {
			return password, nil
		} else if tries < opts.Retries {
			fmt.Fprintf(terminal, "%v: error: passwords does not match. try again.\n", NAME)
			tries++
		} else {
			return nil, errors.New("passwords does not match")
		}
	}
}

func encrypt(opts *Options) (err error) {
	var r io.Reader = os.Stdin
	if opts.Input != "-" {
		f, err2 := os.Open(opts.Input)
		if err2 != nil {
			return err2
		}
		defer func() {
			err = errors.Join(err, f.Close())
		}()
		r = f
	}

	password, err2 := getPassword(opts)
	if err2 != nil {
		return err2
	}

	plaintext, err2 := io.ReadAll(r)
	if err2 != nil {
		return err2
	}

	ciphertext, err2 := goenc.Encrypt(password, plaintext, &goenc.Options{
		Time:    opts.Time,
		Memory:  opts.Memory,
		Threads: opts.Threads,
	})
	if err2 != nil {
		return err2
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		f, err2 := os.OpenFile(opts.Output, flags, 0o666)
		if err2 != nil {
			return err2
		}
		defer func() {
			err = errors.Join(err, f.Close())
		}()
		w = f
	}

	if _, err2 := w.Write(ciphertext); err2 != nil {
		return err2
	}

	return nil
}

func tryDecrypt(opts *Options, input []byte) (plaintext []byte, err error) {
	if opts.PasswordFrom != "" {
		var password []byte
		if opts.PasswordFrom == "-" {
			password, err = io.ReadAll(os.Stdin)
		} else {
			password, err = os.ReadFile(opts.PasswordFrom)
		}
		if err != nil {
			return nil, err
		}
		return goenc.Decrypt(password, input)
	}
	if value, ok := os.LookupEnv("PASSWORD"); ok {
		password := []byte(value)
		return goenc.Decrypt(password, input)
	}

	terminal, err2 := prompt.NewTerminal()
	if err2 != nil {
		return nil, err2
	}
	defer func() {
		err = errors.Join(err, terminal.Close())
	}()

	tries := uint8(1)
	for {
		password, err2 := terminal.ReadPassword(context.Background(), "Password: ")
		if err2 != nil {
			return nil, fmt.Errorf("ReadPassword: %w", err2)
		}
		plaintext, err2 := goenc.Decrypt(password, input)
		if errors.Is(err2, goenc.ErrInvalidTag) && tries < opts.Retries {
			fmt.Fprintf(terminal, "%v: error: incorrect password. try again.\n", NAME)
			tries++
			continue
		} else if err2 != nil {
			return nil, err2
		}
		return plaintext, nil
	}
}

func decrypt(opts *Options) (err error) {
	var r io.Reader = os.Stdin
	if opts.Input != "-" {
		f, err2 := os.Open(opts.Input)
		if err2 != nil {
			return err2
		}
		defer func() {
			err = errors.Join(err, f.Close())
		}()
		r = f
	}

	input, err2 := io.ReadAll(r)
	if err2 != nil {
		return err2
	}

	plaintext, err2 := tryDecrypt(opts, input)
	if err2 != nil {
		return err2
	}

	var w io.Writer = os.Stdout
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		f, err2 := os.OpenFile(opts.Output, flags, 0o666)
		if err2 != nil {
			return err2
		}
		defer func() {
			err = errors.Join(err, f.Close())
		}()
		w = f
	}

	if _, err2 := w.Write(plaintext); err2 != nil {
		return err2
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
	} else if opts.PasswordFrom == "-" && opts.Input == "-" {
		return options.Errorf("cannot read both password and input from stdin")
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
