// Copyright (c) 2020-2026 cions <cions@tuta.com>
// SPDX-License-Identifier: MIT

package goenc

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"math/rand/v2"
	"testing"
)

func TestEncrypt(t *testing.T) {
	options := map[string]*Options{
		"T=1,M=1K,P=1":  {Time: 1, Memory: 1, Threads: 1},
		"T=8,M=64M,P=4": {Time: 8, Memory: 64 << 10, Threads: 4},
	}
	tests := []struct {
		password, plaintext string
	}{
		{"", ""},
		{"0000000000000000", "0000000000000000000000000000000000000000000000000000000000000000"},
		{"0001020304050607", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
		{"70617373776f7264", "476f70686572732c20676f70686572732c20676f7068657273206576657279776865726521"},
	}

	for name, opts := range options {
		t.Run(name, func(t *testing.T) {
			for i, tt := range tests {
				password, err := hex.DecodeString(tt.password)
				if err != nil {
					panic(err)
				}
				plaintext, err := hex.DecodeString(tt.plaintext)
				if err != nil {
					panic(err)
				}
				opts.MaxTime = 16
				opts.MaxMemory = 128 << 10

				encrypted, err := Encrypt(password, plaintext, opts)
				if err != nil {
					t.Errorf("#%d: Encrypt: unexpected error: %v", i, err)
					continue
				}

				decrypted, err := Decrypt(password, encrypted, opts)
				if err != nil {
					t.Errorf("#%d: Decrypt: unexpected error: %v", i, err)
					continue
				}

				if !bytes.Equal(plaintext, decrypted) {
					t.Errorf("#%d: plaintexts does not match: want %x, but got %x", i, plaintext, decrypted)
				}

				if _, err := Decrypt([]byte("wrong password"), encrypted, opts); err == nil {
					t.Errorf("#%d: Decrypt was successful with wrong password", i)
				}

				pos := rand.N(len(encrypted))
				encrypted[pos] ^= 0x80
				if _, err := Decrypt(password, encrypted, opts); err == nil {
					t.Errorf("#%d: Decrypt was successful with tampered ciphertext", i)
				}
			}
		})
	}
}

func TestDecrypt(t *testing.T) {
	opts := &Options{
		MaxTime:   128,
		MaxMemory: 128 << 10,
	}

	tests := []struct {
		password, plaintext, input string
	}{
		{"", "", "AQEAAAABAAAAAdgcG6TIs+wqqJE50jt8VEWRhqHh1On9o58wL4hnLdpp+gyCC7WtSu2EPyo3bbDBZcxzS4Uy3kS8"},
		{"", "", "AQgAAAAAAAEABDj1te6zdktEpLMb6f63K28MmYC/t1KF7waKSJzfDnl2MYcjFKIxgFtdP9N5l7uD0C66HAALgVSb"},
		{
			"password",
			"Gophers, gophers, gophers everywhere!",
			"AQIAAAAAgAAAAfqqqj2YcqCath1bcwxH0Ivz8ax5TZXpd8BAjDYsj5XUgink+DjQQ0k9W+02tNmB" +
				"Y1W5YCXmaLiTAbn4YhEAktj6jyzBXo1A/cQ45K56YZpFOR5rAMMI2om4D7YHmA==",
		},
	}
	for i, tt := range tests {
		password := []byte(tt.password)
		plaintext := []byte(tt.plaintext)
		input, err := base64.StdEncoding.DecodeString(tt.input)
		if err != nil {
			panic(err)
		}

		decrypted, err := Decrypt(password, input, opts)
		if err != nil {
			t.Errorf("#%d: Decrypt: unexpected error: %v", i, err)
			continue
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("#%d: plaintexts does not match: want %x, but got %x", i, plaintext, decrypted)
		}

		if _, err := Decrypt([]byte("wrong password"), input, opts); err == nil {
			t.Errorf("#%d: Decrypt was successful with wrong password", i)
		}

		pos := rand.N(len(input))
		input[pos] ^= 0x80
		if _, err := Decrypt(password, input, opts); err == nil {
			t.Errorf("#%d: Decrypt was successful with tampered ciphertext", i)
		}
	}

	if _, err := Decrypt([]byte{}, []byte{}, opts); err != io.ErrUnexpectedEOF {
		t.Errorf("expected ErrUnexpectedEOF, but got %v", err)
	}

	if _, err := Decrypt([]byte{}, []byte{0x01, 0x02}, opts); err != io.ErrUnexpectedEOF {
		t.Errorf("expected ErrUnexpectedEOF, but got %v", err)
	}

	if _, err := Decrypt([]byte{}, []byte{0x00}, opts); err != ErrFormat {
		t.Errorf("expected ErrFormat, but got %v", err)
	}

	// T=1024,M=64M,P=1
	input, err := base64.StdEncoding.DecodeString("AQAEAAAAAAEAAWFqH7u5nGFBgF70CaDQ4XceR6qdg8aonxONJZFW1odaQvcKAgvl3HdO3u1VzzmT5G9rs4czy6hT")
	if err != nil {
		panic(err)
	}
	if _, err := Decrypt([]byte{}, input, nil); err == nil {
		t.Errorf("Decrypt should return an error when time parameter exceeds MaxTime")
	}

	// T=1,M=32G,P=1
	input, err = base64.StdEncoding.DecodeString("AQEAAAAAAAACAe9imVkApgD+dE2xbWNTdw3kZH0vb/3t7a1aykwIjnJ/lQUXXrVzK2lg6C+AB5rjfhZBTPY1cZaS")
	if err != nil {
		panic(err)
	}
	if _, err := Decrypt([]byte{}, input, nil); err == nil {
		t.Errorf("Decrypt should return an error when memory parameter exceeds MaxMemory")
	}
}
