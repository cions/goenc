// Copyright (c) 2020 cions
// Licensed under the MIT License. See LICENSE for details

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"

	"github.com/mattn/go-tty"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const saltSize = 16

var errInvalidTag = errors.New("message authentication failed (password is wrong or data is corrupted)")

var version = "v0.1.0"

func getPassword(confirm bool) ([]byte, error) {
	if val, ok := os.LookupEnv("PASSWORD"); ok {
		return []byte(val), nil
	}

	tty, err := tty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close()

	if _, err := tty.Output().WriteString("Password: "); err != nil {
		return nil, err
	}
	password, err := tty.ReadPassword()
	if err != nil {
		return nil, err
	}

	if confirm {
		if _, err := tty.Output().WriteString("Confirm Password: "); err != nil {
			return nil, err
		}
		confirmPassword, err := tty.ReadPassword()
		if err != nil {
			return nil, err
		}
		if password != confirmPassword {
			return nil, errors.New("Passwords does not match")
		}
	}

	return []byte(password), nil
}

func encrypt(r io.Reader, w io.Writer, opts *options) error {
	password, err := getPassword(true)
	if err != nil {
		return err
	}

	header := new(bytes.Buffer)
	header.WriteByte(1)
	binary.Write(header, binary.LittleEndian, opts.Time)
	binary.Write(header, binary.LittleEndian, opts.Memory)
	binary.Write(header, binary.LittleEndian, opts.Threads)

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	header.Write(salt)

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	plaintext, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	var dst []byte
	if cap(plaintext) >= len(plaintext)+aead.Overhead() {
		dst = plaintext[:0]
	}
	ciphertext := aead.Seal(dst, nonce, plaintext, header.Bytes())

	if _, err := header.WriteTo(w); err != nil {
		return err
	}
	if _, err := w.Write(nonce); err != nil {
		return err
	}
	if _, err := w.Write(ciphertext); err != nil {
		return err
	}

	return nil
}

func decrypt(r io.Reader, w io.Writer, opts *options) error {
	var version uint8
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("Invalid file format")
	}

	password, err := getPassword(false)
	if err != nil {
		return err
	}

	header := new(bytes.Buffer)
	header.WriteByte(1)

	if err := binary.Read(r, binary.LittleEndian, &opts.Time); err != nil {
		return err
	}
	binary.Write(header, binary.LittleEndian, opts.Time)

	if err := binary.Read(r, binary.LittleEndian, &opts.Memory); err != nil {
		return err
	}
	binary.Write(header, binary.LittleEndian, opts.Memory)

	if err := binary.Read(r, binary.LittleEndian, &opts.Threads); err != nil {
		return err
	}
	binary.Write(header, binary.LittleEndian, opts.Threads)

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(r, salt); err != nil {
		return err
	}
	header.Write(salt)

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}

	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return err
	}

	ciphertext, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	if len(ciphertext) < aead.Overhead() {
		return io.ErrUnexpectedEOF
	}

	plaintext, err := aead.Open(ciphertext[:0], nonce, ciphertext, header.Bytes())
	if err != nil {
		return errInvalidTag
	}

	w.Write(plaintext)

	return nil
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
		os.Exit(2)
	}

	if opts.Operation == opHelp {
		fmt.Println(HelpMessage)
		os.Exit(0)
	}
	if opts.Operation == opVersion {
		fmt.Printf("goenc %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
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

	if opts.Operation == opEncrypt {
		if err := encrypt(r, w, opts); err != nil {
			fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
			os.Exit(2)
		}
	} else {
		if err := decrypt(r, w, opts); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			fmt.Fprintf(os.Stderr, "goenc: error: %v\n", err)
			if errors.Is(err, errInvalidTag) {
				os.Exit(1)
			}
			os.Exit(2)
		}
	}
}
