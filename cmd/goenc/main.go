// Copyright (c) 2020-2023 cions
// Licensed under the MIT License. See LICENSE for details

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/cions/goenc"
	"github.com/cions/goenc/prompt"
)

func main() {
	if err := goenc.Main(os.Args); err != nil {
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
