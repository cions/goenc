// Copyright (c) 2020 cions
// Licensed under the MIT License. See LICENSE for details

package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"runtime"
	"unicode/utf8"

	"golang.org/x/term"
)

type action int

const (
	actInsertChar action = iota
	actIgnore
	actEOF
	actSIGINT
	actSIGQUIT
	actBeginningOfLine
	actEndOfLine
	actBackwardChar
	actForwardChar
	actDeleteBackwardChar
	actDeleteForwardChar
	actKillLine
	actKillWholeLine
	actQuotedInsert
	actRefresh
	actPasteStart
	actPasteEnd
)

var (
	errSIGINT  = errors.New("SIGINT")
	errSIGQUIT = errors.New("SIGQUIT")
)

var (
	mask    = []byte{'*'}
	bs      = []byte{'\b'}
	clr_eos = "\x1b[J"      // Clear to end of screen
	ebp     = "\x1b[?2004h" // Enable Bracketed Paste Mode
	dbp     = "\x1b[?2004l" // Disable Bracketed Paste Mode
)

type tty interface {
	io.Reader
	io.Writer
	io.Closer
	MakeRaw() (*term.State, error)
	Restore(*term.State) error
}

type passwordReader struct {
	tty
}

func scanToken(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if data[0] != '\x1b' {
		if !atEOF && !utf8.FullRune(data) {
			return 0, nil, nil
		}
		_, n := utf8.DecodeRune(data)
		return n, data[:n], nil
	}
	if len(data) >= 3 && data[1] == '[' {
		i := 2
		for i < len(data) && ('0' <= data[i] && data[i] <= '9' || data[i] == ';') {
			i++
		}
		if i < len(data) && ('A' <= data[i] && data[i] <= 'Z' || data[i] == '~') {
			return i + 1, data[:i+1], nil
		}
	} else if len(data) >= 3 && data[1] == 'O' && ('A' <= data[2] && data[2] <= 'Z') {
		return 3, data[:3], nil
	}
	return 1, data[:1], nil
}

func tokenToAction(token []byte, inPaste bool) action {
	if inPaste {
		if bytes.Equal(token, []byte{'\x1b', '[', '2', '0', '1', '~'}) {
			return actPasteEnd
		}
		return actInsertChar
	}

	if 0x20 <= token[0] && token[0] != 0x7f {
		return actInsertChar
	}

	switch token[0] {
	case 0x01: // ^A
		return actBeginningOfLine
	case 0x02: // ^B
		return actBackwardChar
	case 0x03: // ^C
		return actSIGINT
	case 0x04: // ^D
		return actEOF
	case 0x05: // ^E
		return actEndOfLine
	case 0x06: // ^F
		return actForwardChar
	case 0x08: // ^H
		return actDeleteBackwardChar
	case 0x09: // ^I
		return actInsertChar
	case 0x0a: // ^J
		return actEOF
	case 0x0b: // ^K
		return actKillLine
	case 0x0c: // ^L
		return actRefresh
	case 0x0d: // ^M
		return actEOF
	case 0x15: // ^U
		return actKillWholeLine
	case 0x16: // ^V
		return actQuotedInsert
	case 0x1b: // ^[
		break
	case 0x1c: // ^\
		if runtime.GOOS == "windows" {
			return actIgnore
		}
		return actSIGQUIT
	case 0x7f: // Backspace
		return actDeleteBackwardChar
	default:
		return actIgnore
	}

	switch {
	case bytes.Equal(token, []byte{'\x1b', '[', '1', '~'}):
		return actBeginningOfLine
	case bytes.Equal(token, []byte{'\x1b', '[', '3', '~'}):
		return actDeleteForwardChar
	case bytes.Equal(token, []byte{'\x1b', '[', '4', '~'}):
		return actEndOfLine
	case bytes.Equal(token, []byte{'\x1b', '[', '7', '~'}):
		return actBeginningOfLine
	case bytes.Equal(token, []byte{'\x1b', '[', '8', '~'}):
		return actEndOfLine
	case bytes.Equal(token, []byte{'\x1b', '[', '2', '0', '0', '~'}):
		return actPasteStart
	case bytes.Equal(token, []byte{'\x1b', '[', 'C'}):
		return actForwardChar
	case bytes.Equal(token, []byte{'\x1b', '[', 'D'}):
		return actBackwardChar
	case bytes.Equal(token, []byte{'\x1b', '[', 'F'}):
		return actEndOfLine
	case bytes.Equal(token, []byte{'\x1b', '[', 'H'}):
		return actBeginningOfLine
	case bytes.Equal(token, []byte{'\x1b', 'O', 'C'}):
		return actForwardChar
	case bytes.Equal(token, []byte{'\x1b', 'O', 'D'}):
		return actBackwardChar
	case bytes.Equal(token, []byte{'\x1b', 'O', 'F'}):
		return actEndOfLine
	case bytes.Equal(token, []byte{'\x1b', 'O', 'H'}):
		return actBeginningOfLine
	default:
		return actIgnore
	}
}

func newPasswordReader() (*passwordReader, error) {
	tty, err := newTTY()
	if err != nil {
		return nil, err
	}
	return &passwordReader{tty}, nil
}

func (r *passwordReader) ReadPassword(prompt string) ([]byte, error) {
	if _, err := io.WriteString(r, "\r"+clr_eos+ebp+prompt); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(r)
	scanner.Split(scanToken)
	password := make([]byte, 0, 256)
	pos := 0
	inPaste := false

	state, err := r.MakeRaw()
	if err != nil {
		return nil, err
	}
	defer func() {
		if pos < len(password) {
			r.Write(bytes.Repeat(mask, utf8.RuneCount(password[pos:])))
		}
		io.WriteString(r, "\r\n"+dbp)
		r.Restore(state)
	}()

	for scanner.Scan() {
		token := scanner.Bytes()
		switch action := tokenToAction(token, inPaste); action {
		case actEOF:
			return password, nil
		case actSIGINT:
			return nil, errSIGINT
		case actSIGQUIT:
			return nil, errSIGQUIT
		case actBeginningOfLine:
			if pos > 0 {
				r.Write(bytes.Repeat(bs, utf8.RuneCount(password[:pos])))
				pos = 0
			}
		case actEndOfLine:
			if pos < len(password) {
				r.Write(bytes.Repeat(mask, utf8.RuneCount(password[pos:])))
				pos = len(password)
			}
		case actBackwardChar:
			if pos > 0 {
				r.Write(bs)
				_, n := utf8.DecodeLastRune(password[:pos])
				pos -= n
			}
		case actForwardChar:
			if pos < len(password) {
				r.Write(mask)
				_, n := utf8.DecodeRune(password[pos:])
				pos += n
			}
		case actDeleteBackwardChar:
			if pos > 0 {
				_, n := utf8.DecodeLastRune(password[:pos])
				copy(password[pos-n:], password[pos:])
				password = password[:len(password)-n]
				pos -= n
				n = utf8.RuneCount(password[pos:])
				r.Write(bs)
				r.Write(bytes.Repeat(mask, n))
				io.WriteString(r, clr_eos)
				r.Write(bytes.Repeat(bs, n))
			}
		case actDeleteForwardChar:
			if pos < len(password) {
				_, n := utf8.DecodeRune(password[pos:])
				copy(password[pos:], password[pos+n:])
				password = password[:len(password)-n]
				n = utf8.RuneCount(password[pos:])
				r.Write(bytes.Repeat(mask, n))
				io.WriteString(r, clr_eos)
				r.Write(bytes.Repeat(bs, n))
			}
		case actKillLine:
			password = password[:pos]
			io.WriteString(r, clr_eos)
		case actKillWholeLine:
			n := utf8.RuneCount(password[:pos])
			r.Write(bytes.Repeat(bs, n))
			io.WriteString(r, clr_eos)
			password = password[:0]
			pos = 0
		case actRefresh:
			n := utf8.RuneCount(password[:pos])
			r.Write(bytes.Repeat(bs, n))
			io.WriteString(r, "\r"+clr_eos+prompt)
			r.Write(bytes.Repeat(mask, utf8.RuneCount(password)))
			r.Write(bytes.Repeat(bs, utf8.RuneCount(password[pos:])))
		case actPasteStart:
			inPaste = true
		case actPasteEnd:
			inPaste = false
		case actQuotedInsert:
			if scanner.Scan() {
				token = scanner.Bytes()
			}
			fallthrough
		case actInsertChar:
			if pos == len(password) {
				password = append(password, token...)
				pos = len(password)
				n := utf8.RuneCount(token)
				r.Write(bytes.Repeat(mask, n))
			} else {
				newlen := len(password) + len(token)
				if newlen > cap(password) {
					newPassword := make([]byte, 2*newlen)
					copy(newPassword, password)
					password = newPassword
				}
				password = password[:newlen]
				copy(password[pos+len(token):], password[pos:])
				copy(password[pos:], token)
				n := utf8.RuneCount(password[pos:])
				r.Write(bytes.Repeat(mask, n))
				pos += len(token)
				n = utf8.RuneCount(password[pos:])
				r.Write(bytes.Repeat(bs, n))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return password, nil
}