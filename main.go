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
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/mattn/go-tty"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const saltSize = 16

var errInvalidTag = errors.New("message authentication failed")

var version = "v0.1.0"

type memory uint32

func (m *memory) UnmarshalFlag(s string) error {
	var unit uint64 = 1
	width := 32
	if strings.HasSuffix(s, "k") {
		s = strings.TrimSuffix(s, "k")
	} else if strings.HasSuffix(s, "M") {
		s = strings.TrimSuffix(s, "M")
		unit = 1024
		width -= 10
	} else if strings.HasSuffix(s, "G") {
		s = strings.TrimSuffix(s, "G")
		unit = 1024 * 1024
		width -= 20
	}
	i, err := strconv.ParseUint(s, 10, width)
	if err != nil {
		return err
	}
	*m = memory(i * unit)
	return nil
}

type options struct {
	Decrypt bool   `short:"d" long:"decrypt" description:"Decrypt data"`
	Time    uint32 `short:"t" long:"time" default:"8" value-name:"N" description:"Argon2 time parameter"`
	Memory  memory `short:"m" long:"memory" default:"1G" value-name:"N[kMG]" description:"Argon2 memory parameter"`
	Threads uint8  `short:"p" long:"paralellism" default:"4" value-name:"N" description:"Argon2 paralellism parameter"`
	Version bool   `long:"version" description:"Show version and exit"`
}

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

	key := argon2.IDKey(password, salt, opts.Time, uint32(opts.Memory), opts.Threads, chacha20poly1305.KeySize)

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

	key := argon2.IDKey(password, salt, opts.Time, uint32(opts.Memory), opts.Threads, chacha20poly1305.KeySize)

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
	opts := &options{}
	if _, err := flags.Parse(opts); err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if opts.Version {
		fmt.Printf("goenc %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	if !opts.Decrypt {
		err := encrypt(os.Stdin, os.Stdout, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "goenc: %v\n", err)
			os.Exit(2)
		}
	} else {
		err := decrypt(os.Stdin, os.Stdout, opts)
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			fmt.Fprintf(os.Stderr, "goenc: %v\n", err)
			if errors.Is(err, errInvalidTag) {
				os.Exit(1)
			}
			os.Exit(2)
		}
	}
}