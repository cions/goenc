// Copyright (c) 2020-2021 cions
// Licensed under the MIT License. See LICENSE for details

package goenc

import (
	"crypto/rand"
	"encoding/binary"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	saltSizeV1   = 16
	headerSizeV1 = 10 + saltSizeV1
	nonceSizeV1  = chacha20poly1305.NonceSizeX
	minSizeV1    = headerSizeV1 + nonceSizeV1 + chacha20poly1305.Overhead
)

func encryptV1(password, plaintext []byte, opts *options) ([]byte, []byte, error) {
	header := make([]byte, headerSizeV1+nonceSizeV1)
	header[0] = 0x01
	binary.LittleEndian.PutUint32(header[1:5], opts.Time)
	binary.LittleEndian.PutUint32(header[5:9], opts.Memory)
	header[9] = opts.Threads
	if _, err := rand.Read(header[10:]); err != nil {
		return nil, nil, err
	}
	salt := header[10:headerSizeV1]
	nonce := header[headerSizeV1:]

	key := argon2.IDKey(password, salt, opts.Time, opts.Memory, opts.Threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, err
	}

	var dst []byte
	if len(plaintext)+aead.Overhead() <= cap(plaintext) {
		dst = plaintext[:0]
	}
	ciphertext := aead.Seal(dst, nonce, plaintext, header[:headerSizeV1])

	return header, ciphertext, nil
}

func decryptV1(password, input []byte, opts *options) ([]byte, error) {
	header := input[:headerSizeV1]
	time := binary.LittleEndian.Uint32(header[1:5])
	memory := binary.LittleEndian.Uint32(header[5:9])
	threads := header[9]
	salt := header[10:]
	nonce := input[headerSizeV1 : headerSizeV1+nonceSizeV1]
	ciphertext := input[headerSizeV1+nonceSizeV1:]

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
