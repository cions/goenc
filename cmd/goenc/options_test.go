// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

package main

import (
	"reflect"
	"testing"
)

func testParseArgs(t *testing.T, args []string, expected ...any) {
	t.Helper()

	opts := &options{
		Operation: opEncrypt,
		NoClobber: false,
		Time:      8,
		Memory:    1 << 20,
		Threads:   4,
		Retries:   3,
		Input:     "-",
		Output:    "-",
	}

	err := opts.ParseArgs(args)
	if err != nil {
		if expected[0].(string) != "err" {
			t.Errorf("%v: unexpected error: %v", args, err)
		} else if got, want := err.Error(), expected[1].(string); got != want {
			t.Errorf("%v: err = %q, want %q", args, got, want)
		}
		return
	}
	if expected[0].(string) == "err" {
		want := expected[1]
		t.Errorf("%v: err = nil, want %q", args, want)
		return
	}

	v := reflect.ValueOf(opts).Elem()
	for len(expected) > 0 {
		name := expected[0].(string)
		field := v.FieldByName(name)
		switch name {
		case "Operation":
			if got, want := operation(field.Int()), expected[1].(operation); got != want {
				t.Errorf("%v: %v = %v, want %v", args, name, got, want)
			}
		case "NoClobber":
			if got, want := field.Bool(), expected[1].(bool); got != want {
				t.Errorf("%v: %v = %v, want %v", args, name, got, want)
			}
		case "Time", "Memory":
			if got, want := uint32(field.Uint()), expected[1].(uint32); got != want {
				t.Errorf("%v: %v = %v, want %v", args, name, got, want)
			}
		case "Threads", "Retries":
			if got, want := uint8(field.Uint()), expected[1].(uint8); got != want {
				t.Errorf("%v: %v = %v, want %v", args, name, got, want)
			}
		case "Input", "Output":
			if got, want := field.String(), expected[1].(string); got != want {
				t.Errorf("%v: %v = %q, want %q", args, name, got, want)
			}
		default:
			panic("invalid field: " + name)
		}
		expected = expected[2:]
	}
}

func TestParseArgs(t *testing.T) {
	testParseArgs(t, []string{},
		"Operation", opEncrypt,
		"NoClobber", false,
		"Time", uint32(8),
		"Memory", uint32(1<<20),
		"Threads", uint8(4),
		"Retries", uint8(3),
		"Input", "-",
		"Output", "-",
	)

	testParseArgs(t, []string{"-d", "-n", "-t128", "-m64M", "-p8", "-r", "5", "input", "output"},
		"Operation", opDecrypt,
		"NoClobber", true,
		"Time", uint32(128),
		"Memory", uint32(64<<10),
		"Threads", uint8(8),
		"Retries", uint8(5),
		"Input", "input",
		"Output", "output",
	)

	testParseArgs(t, []string{"-ent", "65536", "--memory=1G", "--parallelism", "128"},
		"Operation", opEncrypt,
		"NoClobber", true,
		"Time", uint32(65536),
		"Memory", uint32(1<<20),
		"Threads", uint8(128),
	)

	testParseArgs(t, []string{"input", "-m64K", "output", "--time=10"},
		"Time", uint32(10),
		"Memory", uint32(64),
		"Input", "input",
		"Output", "output",
	)

	testParseArgs(t, []string{"-d", "--", "-input", "--output"},
		"Operation", opDecrypt,
		"Input", "-input",
		"Output", "--output",
	)

	testParseArgs(t, []string{"-h"}, "Operation", opHelp)
	testParseArgs(t, []string{"--help"}, "Operation", opHelp)
	testParseArgs(t, []string{"--version"}, "Operation", opVersion)

	testParseArgs(t, []string{"first", "second", "third"}, "err", "too many arguments")

	testParseArgs(t, []string{"-a"}, "err", "unknown option '-a'")
	testParseArgs(t, []string{"-encrypt"}, "err", "unknown option '-c'")
	testParseArgs(t, []string{"-d-"}, "err", "invalid option '-'")
	testParseArgs(t, []string{"--sign"}, "err", "unknown option '--sign'")
	testParseArgs(t, []string{"--recipient=name"}, "err", "unknown option '--recipient'")

	testParseArgs(t, []string{"-t"}, "err", "option -t requires an argument")
	testParseArgs(t, []string{"--retries"}, "err", "option --retries requires an argument")
	testParseArgs(t, []string{"--encrypt=true"}, "err", "option --encrypt takes no argument")

	testParseArgs(t, []string{"-t", "nan"}, "err", "option -t: invalid number")
	testParseArgs(t, []string{"-m1T"}, "err", "option -m: invalid number")
	testParseArgs(t, []string{"-p65536"}, "err", "option -p: value out of range")
	testParseArgs(t, []string{"--retries=0"}, "err", "option --retries: value out of range")
	testParseArgs(t, []string{"--memory", "4294967296"}, "err", "option --memory: value out of range")
	testParseArgs(t, []string{"--memory=0"}, "err", "option --memory: value out of range")
}
