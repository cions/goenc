// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const helpMessage = `usage: goenc [options] [input] [output]

A simple file encryption tool

Options:
 -e, --encrypt          Encrypt
 -d, --decrypt          Decrypt
 -n, --no-clobber       Do not overwrite an existing file
 -t, --time=N           Argon2 time parameter (default: 8)
 -m, --memory=N[kMG]    Argon2 memory parameter (default: 1G)
 -p, --parallelism=N    Argon2 parallelism parameter (default: 4)
 -h, --help             Show this help message and exit
     --version          Show version information and exit

Environment Variable:
  PASSWORD              Encryption password

Exit Status:
  0  Operation was successful
  1  Message authentication failed (password is wrong or data is corrupted)
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
	Input     string
	Output    string
}

var takeValue = map[string]bool{
	"-e":            false,
	"--encrypt":     false,
	"-d":            false,
	"--decrypt":     false,
	"-n":            false,
	"--no-clobber":  false,
	"-t":            true,
	"--time":        true,
	"-m":            true,
	"--memory":      true,
	"-p":            true,
	"--parallelism": true,
	"-h":            false,
	"--help":        false,
	"--version":     false,
}

func parseArgs(args []string) (*options, error) {
	opts := &options{
		Operation: opEncrypt,
		NoClobber: false,
		Time:      8,
		Memory:    1 * 1024 * 1024,
		Threads:   4,
		Input:     "-",
		Output:    "-",
	}

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
			args = args[len(args):]
			continue
		case strings.HasPrefix(args[0], "--"):
			if idx := strings.IndexByte(args[0], '='); idx >= 0 {
				name = args[0][:idx]
				value = args[0][idx+1:]
				if b, ok := takeValue[name]; ok && !b {
					return nil, fmt.Errorf("option %s takes no value", name)
				}
				args = args[1:]
			} else {
				name = args[0]
				if takeValue[name] {
					if len(args) == 1 {
						return nil, fmt.Errorf("option %s requires a value", name)
					}
					value = args[1]
					args = args[2:]
				} else {
					args = args[1:]
				}
			}
		default:
			name = args[0][:2]
			if len(args[0]) > 2 {
				if b, ok := takeValue[name]; b {
					value = args[0][2:]
					args = args[1:]
				} else if ok && args[0][2] == '-' {
					return nil, fmt.Errorf("option %s takes no value", name)
				} else {
					args[0] = "-" + args[0][2:]
				}
			} else {
				if takeValue[name] {
					if len(args) == 1 {
						return nil, fmt.Errorf("option %s requires a value", name)
					}
					value = args[1]
					args = args[2:]
				} else {
					args = args[1:]
				}
			}
		}
		switch name {
		case "-e", "--encrypt":
			opts.Operation = opEncrypt
		case "-d", "--decrypt":
			opts.Operation = opDecrypt
		case "-n", "--no-clobber":
			opts.NoClobber = true
		case "-t", "--time":
			v, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				if errors.Is(err, strconv.ErrSyntax) {
					return nil, fmt.Errorf("option %s expects a number", name)
				}
				if errors.Is(err, strconv.ErrRange) {
					return nil, fmt.Errorf("option %s: value out of range", name)
				}
				return nil, fmt.Errorf("option %s: %w", name, err)
			}
			opts.Time = uint32(v)
		case "-m", "--memory":
			unit := uint64(1)
			width := 32
			if strings.HasSuffix(value, "k") {
				value = strings.TrimSuffix(value, "k")
			} else if strings.HasSuffix(value, "M") {
				value = strings.TrimSuffix(value, "M")
				unit = 1024
				width -= 10
			} else if strings.HasSuffix(value, "G") {
				value = strings.TrimSuffix(value, "G")
				unit = 1024 * 1024
				width -= 20
			}
			v, err := strconv.ParseUint(value, 10, width)
			if err != nil {
				if errors.Is(err, strconv.ErrSyntax) {
					return nil, fmt.Errorf("option %s expects a number (with optional suffix k, M or G)", name)
				}
				if errors.Is(err, strconv.ErrRange) {
					return nil, fmt.Errorf("option %s: value out of range", name)
				}
				return nil, fmt.Errorf("option %s: %w", name, err)
			}
			opts.Memory = uint32(v * unit)
		case "-p", "--parallelism":
			v, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				if errors.Is(err, strconv.ErrSyntax) {
					return nil, fmt.Errorf("option %s expects a number", name)
				}
				if errors.Is(err, strconv.ErrRange) {
					return nil, fmt.Errorf("option %s: value out of range", name)
				}
				return nil, fmt.Errorf("option %s: %w", name, err)
			}
			opts.Threads = uint8(v)
		case "-h", "--help":
			opts.Operation = opHelp
			return opts, nil
		case "--version":
			opts.Operation = opVersion
			return opts, nil
		default:
			return nil, fmt.Errorf("unknown option '%s'", name)
		}
	}
	if len(posargs) >= 1 {
		opts.Input = posargs[0]
	}
	if len(posargs) >= 2 {
		opts.Output = posargs[1]
	}
	if len(posargs) >= 3 {
		return nil, errors.New("too many arguments")
	}
	return opts, nil
}
