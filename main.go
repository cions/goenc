// Copyright (c) 2020-2026 cions <cions@tuta.com>
// SPDX-License-Identifier: MIT

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

const (
	DefaultTime      = 8
	DefaultMemory    = 256 << 10 // 256 MiB
	DefaultThreads   = 4
	DefaultMaxTime   = 128
	DefaultMaxMemory = 4 << 20 // 4 GiB
)

const (
	SaltSize        = 16
	HeaderSize      = 10 + SaltSize
	NonceSize       = chacha20poly1305.NonceSizeX
	CiphertextStart = HeaderSize + NonceSize
	MinSize         = HeaderSize + NonceSize + chacha20poly1305.Overhead
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

// Options are encryption/decryption parameters.
type Options struct {
	Format    Format // File format
	Time      uint32 // KDF time parameter
	MaxTime   uint32 // Upper limit for KDF time parameter
	Memory    uint32 // KDF memory parameter
	MaxMemory uint32 // Upper limit for KDF memory parameter
	Threads   uint8  // KDF parallelism parameter
}

func setDefaultOptions(opts *Options) *Options {
	if opts == nil {
		opts = &Options{}
	}
	if opts.Format == FormatDefault {
		opts.Format = FormatV1
	}
	if opts.Time == 0 {
		opts.Time = DefaultTime
	}
	if opts.MaxTime == 0 {
		opts.MaxTime = DefaultMaxTime
	}
	if opts.Memory == 0 {
		opts.Memory = DefaultMemory
	}
	if opts.MaxMemory == 0 {
		opts.MaxMemory = DefaultMaxMemory
	}
	if opts.Threads == 0 {
		opts.Threads = DefaultThreads
	}
	return opts
}

// Encrypt encrypts plaintext with password.
func Encrypt(password, plaintext []byte, opts *Options) ([]byte, error) {
	opts = setDefaultOptions(opts)
	if opts.Format != FormatV1 {
		return nil, fmt.Errorf("unsupported Format (%v)", opts.Format)
	}
	if opts.Time > opts.MaxTime {
		return nil, fmt.Errorf("time parameter (%v) exceeds the upper limit (%v)", opts.Time, opts.MaxTime)
	}
	if opts.Memory > opts.MaxMemory {
		return nil, fmt.Errorf("memory parameter (%v) exceeds the upper limit (%v)", opts.Memory, opts.MaxMemory)
	}

	buf := make([]byte, MinSize+len(plaintext))
	header := buf[:HeaderSize]
	salt := buf[HeaderSize-SaltSize : HeaderSize]
	nonce := buf[HeaderSize : HeaderSize+NonceSize]
	dst := buf[:CiphertextStart]

	header[0] = 0x01
	binary.LittleEndian.PutUint32(header[1:5], opts.Time)
	binary.LittleEndian.PutUint32(header[5:9], opts.Memory)
	header[9] = opts.Threads
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("crypto/rand: %w", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto/rand: %w", err)
	}

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(dst, nonce, plaintext, header), nil
}

// Decrypt decrypts input with password.
func Decrypt(password, input []byte, opts *Options) ([]byte, error) {
	opts = setDefaultOptions(opts)

	if len(input) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if input[0] != 0x01 {
		return nil, ErrFormat
	}
	if len(input) < MinSize {
		return nil, io.ErrUnexpectedEOF
	}

	header := input[:HeaderSize]
	time := binary.LittleEndian.Uint32(header[1:5])
	memory := binary.LittleEndian.Uint32(header[5:9])
	threads := header[9]
	salt := header[HeaderSize-SaltSize:]
	nonce := input[HeaderSize : HeaderSize+NonceSize]
	ciphertext := input[CiphertextStart:]

	if time > opts.MaxTime {
		return nil, fmt.Errorf("time parameter (%v) exceeds the upper limit (%v)", time, opts.MaxTime)
	}
	if memory > opts.MaxMemory {
		return nil, fmt.Errorf("memory parameter (%v) exceeds the upper limit (%v)", memory, opts.MaxMemory)
	}

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
