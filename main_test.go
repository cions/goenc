// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

package goenc

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"math/rand"
	"testing"
)

func TestEncrypt(t *testing.T) {
	f := func(t *testing.T, opts *Options) {
		cases := []struct {
			password, plaintext string
		}{
			{"", ""},
			{"0000000000000000", "0000000000000000000000000000000000000000000000000000000000000000"},
			{"0001020304050607", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
			{"70617373776f7264", "476f70686572732c20676f70686572732c20676f7068657273206576657279776865726521"},
		}

		for i, tc := range cases {
			password, _ := hex.DecodeString(tc.password)
			plaintext, _ := hex.DecodeString(tc.plaintext)

			encrypted, err := Encrypt(password, plaintext, opts)
			if err != nil {
				t.Errorf("#%d: Encrypt failed: %v", i, err)
				continue
			}

			decrypted, err := Decrypt(password, encrypted)
			if err != nil {
				t.Errorf("#%d: Decrypt failed: %v", i, err)
				continue
			}

			if !bytes.Equal(plaintext, decrypted) {
				t.Errorf("#%d: plaintexts does not match: %x vs %x", i, plaintext, decrypted)
			}

			if _, err = Decrypt([]byte("wrong"), encrypted); err == nil {
				t.Errorf("#%d: Decrypt was successful with wrong password", i)
			}

			idx := rand.Intn(len(encrypted)-10) + 10
			encrypted[idx] ^= 0x80
			if _, err = Decrypt(password, encrypted); err == nil {
				t.Errorf("#%d: Decrypt was successful after altering ciphertext", i)
			}
		}
	}

	t.Run("T=1,M=1,P=1", func(t *testing.T) {
		f(t, &Options{Time: 1, Memory: 1, Threads: 1})
	})
	t.Run("T=8,M=64M,P=4", func(t *testing.T) {
		f(t, &Options{Time: 8, Memory: 64 * 1024, Threads: 4})
	})
}

func TestDecrypt(t *testing.T) {
	cases := []struct {
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

	for i, tc := range cases {
		password := []byte(tc.password)
		plaintext := []byte(tc.plaintext)
		input, _ := base64.StdEncoding.DecodeString(tc.input)

		decrypted, err := Decrypt(password, input)
		if err != nil {
			t.Errorf("#%d: Decrypt failed: %v", i, err)
			continue
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Errorf("#%d: plaintexts does not match: %x vs %x", i, plaintext, decrypted)
		}

		if _, err = Decrypt([]byte("wrong"), input); err == nil {
			t.Errorf("#%d: Decrypt was successful with wrong password", i)
		}

		idx := rand.Intn(len(input)-10) + 10
		input[idx] ^= 0x80
		if _, err = Decrypt(password, input); err == nil {
			t.Errorf("#%d: Decrypt was successful after altering ciphertext", i)
		}
	}

	if _, err := Decrypt([]byte{}, []byte{}); err != io.ErrUnexpectedEOF {
		t.Errorf("err = %v, want %v", err, io.ErrUnexpectedEOF)
	}

	if _, err := Decrypt([]byte{}, []byte{0x01, 0x02}); err != io.ErrUnexpectedEOF {
		t.Errorf("err = %v, want %v", err, io.ErrUnexpectedEOF)
	}

	if _, err := Decrypt([]byte{}, []byte{0x00}); err != ErrFormat {
		t.Errorf("err = %v, want %v", err, ErrFormat)
	}
}
