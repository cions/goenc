// Copyright (c) 2020-2023 cions
// Licensed under the MIT License. See LICENSE for details.

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const helpMessage = `Usage: goenc [OPTIONS] [INPUT] [OUTPUT]

A simple file encryption tool

Options:
  -e, --encrypt         Encrypt
  -d, --decrypt         Decrypt
  -n, --no-clobber      Do not overwrite an existing file
  -t, --time=N          Argon2 time parameter (default: 8)
  -m, --memory=N[KMG]   Argon2 memory parameter (default: 1G)
  -p, --parallelism=N   Argon2 parallelism parameter (default: 4)
  -r, --retries=N       Maximum number of attempts to enter password (default: 3)
  -h, --help            Show this help message and exit
      --version         Show version information and exit

Environment Variables:
  PASSWORD              Encryption password

Exit Status:
  0  Operation was successful
  1  Tag verification failed (password is wrong or data is corrupted)
  2  An error occurred`

type operation int

const (
	opEncrypt operation = iota
	opDecrypt
	opHelp
	opVersion
)

type options struct {
	Operation operation
	NoClobber bool
	Time      uint32
	Memory    uint32
	Threads   uint8
	Retries   uint8
	Input     string
	Output    string
}

func (*options) IsBoolFlag(name string) (bool, error) {
	switch name {
	case "-e", "--encrypt":
		return true, nil
	case "-d", "--decrypt":
		return true, nil
	case "-n", "--no-clobber":
		return true, nil
	case "-t", "--time":
		return false, nil
	case "-m", "--memory":
		return false, nil
	case "-p", "--parallelism":
		return false, nil
	case "-r", "--retries":
		return false, nil
	case "-h", "--help":
		return true, nil
	case "--version":
		return true, nil
	default:
		return false, fmt.Errorf("unknown option '%s'", name)
	}
}

func (*options) ParseIntFlag(name, value string, width int) (uint64, error) {
	x, err := strconv.ParseUint(value, 10, width)
	switch {
	case errors.Is(err, strconv.ErrSyntax):
		return 0, fmt.Errorf("option %s: invalid number", name)
	case errors.Is(err, strconv.ErrRange), x == 0:
		return 0, fmt.Errorf("option %s: value out of range", name)
	case err != nil:
		return 0, fmt.Errorf("option %s: %w", name, err)
	}
	return x, nil
}

func (*options) ParseSizeFlag(name, value string) (uint32, error) {
	unit := uint64(1)
	width := 32
	switch {
	case strings.HasSuffix(value, "K"):
		value = strings.TrimSuffix(value, "K")
	case strings.HasSuffix(value, "M"):
		value = strings.TrimSuffix(value, "M")
		unit = 1024
		width -= 10
	case strings.HasSuffix(value, "G"):
		value = strings.TrimSuffix(value, "G")
		unit = 1024 * 1024
		width -= 20
	}

	x, err := strconv.ParseUint(value, 10, width)
	switch {
	case errors.Is(err, strconv.ErrSyntax):
		return 0, fmt.Errorf("option %s: invalid number", name)
	case errors.Is(err, strconv.ErrRange), x == 0:
		return 0, fmt.Errorf("option %s: value out of range", name)
	case err != nil:
		return 0, fmt.Errorf("option %s: %w", name, err)
	}
	return uint32(x * unit), nil
}

func (opts *options) VisitFlag(name, value string) error {
	switch name {
	case "-e", "--encrypt":
		opts.Operation = opEncrypt
	case "-d", "--decrypt":
		opts.Operation = opDecrypt
	case "-n", "--no-clobber":
		opts.NoClobber = true
	case "-t", "--time":
		x, err := opts.ParseIntFlag(name, value, 32)
		if err != nil {
			return err
		}
		opts.Time = uint32(x)
	case "-m", "--memory":
		x, err := opts.ParseSizeFlag(name, value)
		if err != nil {
			return err
		}
		opts.Memory = x
	case "-p", "--parallelism":
		x, err := opts.ParseIntFlag(name, value, 8)
		if err != nil {
			return err
		}
		opts.Threads = uint8(x)
	case "-r", "--retries":
		x, err := opts.ParseIntFlag(name, value, 8)
		if err != nil {
			return err
		}
		opts.Retries = uint8(x)
	case "-h", "--help":
		opts.Operation = opHelp
	case "--version":
		opts.Operation = opVersion
	default:
		return fmt.Errorf("unknown option '%s'", name)
	}
	return nil
}

func (opts *options) ParseArgs(args []string) error {
	var posargs []string

	for len(args) > 0 {
		var name, value string
		switch {
		case !strings.HasPrefix(args[0], "-"), args[0] == "-":
			posargs = append(posargs, args[0])
			args = args[1:]
			continue
		case args[0] == "--":
			posargs = append(posargs, args[1:]...)
			args = nil
			continue
		case strings.HasPrefix(args[0], "--"):
			if strings.Contains(args[0], "=") {
				name, value, _ = strings.Cut(args[0], "=")
				if boolFlag, err := opts.IsBoolFlag(name); err != nil {
					return err
				} else if boolFlag {
					return fmt.Errorf("option %s takes no value", name)
				} else {
					args = args[1:]
				}
			} else {
				name = args[0]
				if boolFlag, err := opts.IsBoolFlag(name); err != nil {
					return err
				} else if boolFlag {
					args = args[1:]
				} else {
					if len(args) == 1 {
						return fmt.Errorf("option %s requires a value", name)
					}
					value = args[1]
					args = args[2:]
				}
			}
		default:
			name = args[0][:2]
			if len(args[0]) > 2 {
				if boolFlag, err := opts.IsBoolFlag(name); err != nil {
					return err
				} else if boolFlag {
					args[0] = "-" + args[0][2:]
				} else {
					value = args[0][2:]
					args = args[1:]
				}
			} else {
				if boolFlag, err := opts.IsBoolFlag(name); err != nil {
					return err
				} else if boolFlag {
					args = args[1:]
				} else {
					if len(args) == 1 {
						return fmt.Errorf("option %s requires a value", name)
					}
					value = args[1]
					args = args[2:]
				}
			}
		}
		if err := opts.VisitFlag(name, value); err != nil {
			return err
		}
	}
	if len(posargs) >= 1 {
		opts.Input = posargs[0]
	}
	if len(posargs) >= 2 {
		opts.Output = posargs[1]
	}
	if len(posargs) >= 3 {
		return errors.New("too many arguments")
	}
	return nil
}
