// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/cions/goenc/prompt"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const saltSize = 16

var errInvalidTag = errors.New("message authentication failed (password is wrong or data is corrupted)")

func getVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "(devel)"
}

func getPassword(confirm bool) ([]byte, error) {
	if val, ok := os.LookupEnv("PASSWORD"); ok {
		return []byte(val), nil
	}

	reader, err := prompt.NewReader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	password, err := reader.ReadPassword(context.Background(), "Password: ")
	if err != nil {
		return nil, err
	}

	if confirm {
		confirmPassword, err := reader.ReadPassword(context.Background(), "Confirm Password: ")
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(password, confirmPassword) {
			return nil, errors.New("passwords does not match")
		}
	}

	return password, nil
}

func encrypt(r io.Reader, w io.Writer, opts *options) (n int, err error) {
	password, err := getPassword(true)
	if err != nil {
		return 0, err
	}

	header := new(bytes.Buffer)
	header.WriteByte(1)
	binary.Write(header, binary.LittleEndian, opts.Time)
	binary.Write(header, binary.LittleEndian, opts.Memory)
	binary.Write(header, binary.LittleEndian, opts.Threads)

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return 0, err
	}
	header.Write(salt)

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return 0, err
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return 0, err
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}

	var dst []byte
	if len(plaintext)+aead.Overhead() <= cap(plaintext) {
		dst = plaintext[:0]
	}
	ciphertext := aead.Seal(dst, nonce, plaintext, header.Bytes())

	n1, err := header.WriteTo(w)
	if err != nil {
		return 0, err
	}
	n += int(n1)

	n2, err := w.Write(nonce)
	if err != nil {
		return 0, err
	}
	n += n2

	n3, err := w.Write(ciphertext)
	if err != nil {
		return 0, err
	}
	n += n3

	return n, nil
}

func decrypt(r io.Reader, w io.Writer, opts *options) (n int, err error) {
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	password, err := getPassword(false)
	if err != nil {
		return 0, err
	}

	header := new(bytes.Buffer)

	var version uint8
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return 0, err
	}
	if version != 1 {
		return 0, fmt.Errorf("invalid file format")
	}
	header.WriteByte(version)

	if err := binary.Read(r, binary.LittleEndian, &opts.Time); err != nil {
		return 0, err
	}
	binary.Write(header, binary.LittleEndian, opts.Time)

	if err := binary.Read(r, binary.LittleEndian, &opts.Memory); err != nil {
		return 0, err
	}
	binary.Write(header, binary.LittleEndian, opts.Memory)

	if err := binary.Read(r, binary.LittleEndian, &opts.Threads); err != nil {
		return 0, err
	}
	binary.Write(header, binary.LittleEndian, opts.Threads)

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(r, salt); err != nil {
		return 0, err
	}
	header.Write(salt)

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return 0, err
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return 0, err
	}

	ciphertext, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	if len(ciphertext) < aead.Overhead() {
		return 0, io.ErrUnexpectedEOF
	}

	plaintext, err := aead.Open(ciphertext[:0], nonce, ciphertext, header.Bytes())
	if err != nil {
		return 0, errInvalidTag
	}

	return w.Write(plaintext)
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
		os.Exit(2)
	}

	if opts.Operation == opHelp {
		fmt.Println(helpMessage)
		os.Exit(0)
	}
	if opts.Operation == opVersion {
		fmt.Printf("goenc %s (%s/%s)\n", getVersion(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	var r io.Reader = os.Stdin
	var w io.Writer = os.Stdout
	if opts.Input != "-" {
		fh, err := os.Open(opts.Input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
			os.Exit(2)
		}
		defer fh.Close()
		r = fh
	}
	if opts.Output != "-" {
		flags := os.O_WRONLY | os.O_CREATE
		if opts.NoClobber {
			flags |= os.O_EXCL
		}
		fh, err := os.OpenFile(opts.Output, flags, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
			os.Exit(2)
		}
		defer fh.Close()
		w = fh
	}

	var n int
	if opts.Operation == opEncrypt {
		n, err = encrypt(r, w, opts)
	} else {
		n, err = decrypt(r, w, opts)
	}
	if fh, ok := w.(*os.File); ok && err == nil {
		if stat, err2 := fh.Stat(); err2 == nil && stat.Mode().IsRegular() {
			err = fh.Truncate(int64(n))
		}
	}
	if err != nil {
		if se, ok := err.(*prompt.SignalError); ok {
			os.Exit(128 + se.Signal())
		}
		fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
		if errors.Is(err, errInvalidTag) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}
