// Copyright (c) 2020-2023 cions
// Licensed under the MIT License. See LICENSE for details.

package goenc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	ErrFormat     = errors.New("not a valid goenc file")
	ErrInvalidTag = errors.New("tag verification failed (password is wrong or data is corrupted)")
)

// Format represents the file format.
type Format int

const (
	// FormatDefault indicates that the format is not specified.
	FormatDefault Format = iota

	// FormatV1 represents the goenc version 1 format.
	//
	// Version 1 format uses XChaCha20-Poly1305 for authenticated encryption and Argon2id for key derivation.
	FormatV1
)

// Options are encryption parameters.
type Options struct {
	Format  Format // File format
	Time    uint32 // KDF time parameter
	Memory  uint32 // KDF memory parameter
	Threads uint8  // KDF parallelism parameter
}

// Encrypt encrypts plaintext with password.
func Encrypt(password, plaintext []byte, opts *Options) ([]byte, error) {
	switch opts.Format {
	case FormatDefault, FormatV1:
		return encryptV1(password, plaintext, opts)
	default:
		return nil, fmt.Errorf("opts.Format is invalid: %v", opts.Format)
	}
}

// Decrypt decrypts input with password.
func Decrypt(password, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	switch input[0] {
	case 0x01:
		return decryptV1(password, input)
	default:
		return nil, ErrFormat
	}
}

const (
	saltSizeV1   = 16
	headerSizeV1 = 10 + saltSizeV1
	nonceSizeV1  = chacha20poly1305.NonceSizeX
	ctStartV1    = headerSizeV1 + nonceSizeV1
	minSizeV1    = headerSizeV1 + nonceSizeV1 + chacha20poly1305.Overhead
)

func encryptV1(password, plaintext []byte, opts *Options) ([]byte, error) {
	buf := make([]byte, minSizeV1+len(plaintext))
	header := buf[:headerSizeV1]
	salt := header[10:]
	nonce := buf[headerSizeV1:ctStartV1]
	dst := buf[:ctStartV1]

	header[0] = 0x01
	binary.LittleEndian.PutUint32(header[1:5], opts.Time)
	binary.LittleEndian.PutUint32(header[5:9], opts.Memory)
	header[9] = opts.Threads
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(dst, nonce, plaintext, header), nil
}

func decryptV1(password, input []byte) ([]byte, error) {
	if len(input) < minSizeV1 {
		return nil, io.ErrUnexpectedEOF
	}
	if input[0] != 0x01 {
		return nil, ErrFormat
	}

	header := input[:headerSizeV1]
	time := binary.LittleEndian.Uint32(header[1:5])
	memory := binary.LittleEndian.Uint32(header[5:9])
	threads := header[9]
	salt := header[10:]
	nonce := input[headerSizeV1:ctStartV1]
	ciphertext := input[ctStartV1:]

	key := argon2.IDKey(password, salt, time, memory, threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, header)
	if err != nil {
		return nil, ErrInvalidTag
	}
	return plaintext, nil
}
