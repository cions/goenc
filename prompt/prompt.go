// Copyright (c) 2020-2023 cions
// Licensed under the MIT License. See LICENSE for details

package prompt

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/width"
)

var (
	clreos = "\x1b[J"      // Clear to end of screen
	ebp    = "\x1b[?2004h" // Enable Bracketed Paste Mode
	dbp    = "\x1b[?2004l" // Disable Bracketed Paste Mode
)

// TransformFunc transform src into its display form and backspaces of its display width.
type TransformFunc func(src []byte) (disp, bs []byte)

// CaretNotation displays all control characters in the caret notation.
func CaretNotation(src []byte) ([]byte, []byte) {
	disp := make([]byte, 0, 2*len(src))
	n := 0

	for len(src) > 0 {
		r, size := utf8.DecodeRune(src)
		if r < 0x20 || r == 0x7f {
			disp = append(disp, '^', byte(r)^0x40)
			n += 2
		} else {
			disp = append(disp, src[:size]...)
			switch width.LookupRune(r).Kind() {
			case width.EastAsianWide, width.EastAsianFullwidth:
				n += 2
			default:
				n += 1
			}
		}
		src = src[size:]
	}

	return disp, bytes.Repeat([]byte{'\b'}, n)
}

// Masked displays all characters as asterisk.
func Masked(src []byte) ([]byte, []byte) {
	n := utf8.RuneCount(src)
	return bytes.Repeat([]byte{'*'}, n), bytes.Repeat([]byte{'\b'}, n)
}

// Blanked displays nothing.
func Blanked(src []byte) ([]byte, []byte) {
	return []byte{}, []byte{}
}

// SignalError indicates the operation was interrupted by a signal.
type SignalError syscall.Signal

func (se SignalError) Error() string {
	return syscall.Signal(se).String()
}

// Signal returns the signal number.
func (se SignalError) Signal() int {
	return int(se)
}

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

func isHex(b byte) bool {
	return ('0' <= b && b <= '9') || ('A' <= b && b <= 'F') || ('a' <= b && b <= 'f')
}

func scanToken(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	switch data[0] {
	case 0x16: // ^V
		if len(data) == 1 {
			if !atEOF {
				return 0, nil, nil
			}
			return 1, data[:1], nil
		}
		i := 2
		var maxlen int
		if data[1] == 'x' {
			maxlen = 4
		} else if data[1] == 'u' {
			maxlen = 6
		} else if data[1] == 'U' {
			maxlen = 10
		} else {
			return 1, data[:1], nil
		}
		for i < len(data) && i < maxlen && isHex(data[i]) {
			i++
		}
		if i == len(data) && i < maxlen && !atEOF {
			return 0, nil, nil
		}
		if i == 2 {
			return 1, data[:1], nil
		}
		return i, data[:i], nil
	case 0x1b: // ^[
		if len(data) >= 3 && data[1] == '[' {
			i := 2
			for i < len(data) && ('0' <= data[i] && data[i] <= '9' || data[i] == ';') {
				i++
			}
			if i < len(data) && ('A' <= data[i] && data[i] <= 'Z' || data[i] == '~') {
				i++
				return i, data[:i], nil
			}
		} else if len(data) >= 3 && data[1] == 'O' && ('A' <= data[2] && data[2] <= 'Z') {
			return 3, data[:3], nil
		}
		return 1, data[:1], nil
	default:
		if !atEOF && !utf8.FullRune(data) {
			return 0, nil, nil
		}
		_, n := utf8.DecodeRune(data)
		return n, data[:n], nil
	}
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

func readLine(r io.Reader, w io.Writer, prompt string, transform TransformFunc) ([]byte, error) {
	buffer := make([]byte, 0, 256)
	pos := 0
	inPaste := false

	if _, err := io.WriteString(w, "\r"+clreos+prompt+ebp); err != nil {
		return nil, err
	}

	defer func() {
		if pos < len(buffer) {
			out, _ := transform(buffer[pos:])
			w.Write(out)
		}
		io.WriteString(w, "\r\n"+dbp)
	}()

	scanner := bufio.NewScanner(r)
	scanner.Split(scanToken)
	for scanner.Scan() {
		token := scanner.Bytes()
		switch action := tokenToAction(token, inPaste); action {
		case actEOF:
			return buffer, nil
		case actSIGINT:
			return nil, SignalError(syscall.SIGINT)
		case actSIGQUIT:
			return nil, SignalError(syscall.SIGQUIT)
		case actBeginningOfLine:
			if pos > 0 {
				_, bs := transform(buffer[:pos])
				if _, err := w.Write(bs); err != nil {
					return nil, err
				}
				pos = 0
			}
		case actEndOfLine:
			if pos < len(buffer) {
				out, _ := transform(buffer[pos:])
				if _, err := w.Write(out); err != nil {
					return nil, err
				}
				pos = len(buffer)
			}
		case actBackwardChar:
			if pos > 0 {
				_, n := utf8.DecodeLastRune(buffer[:pos])
				_, bs := transform(buffer[pos-n : pos])
				if _, err := w.Write(bs); err != nil {
					return nil, err
				}
				pos -= n
			}
		case actForwardChar:
			if pos < len(buffer) {
				_, n := utf8.DecodeRune(buffer[pos:])
				out, _ := transform(buffer[pos : pos+n])
				if _, err := w.Write(out); err != nil {
					return nil, err
				}
				pos += n
			}
		case actDeleteBackwardChar:
			if pos > 0 {
				_, n := utf8.DecodeLastRune(buffer[:pos])
				_, bs := transform(buffer[pos-n : pos])
				copy(buffer[pos-n:], buffer[pos:])
				buffer = buffer[:len(buffer)-n]
				pos -= n
				if _, err := w.Write(bs); err != nil {
					return nil, err
				}
				out, bs := transform(buffer[pos:])
				if _, err := w.Write(append(append(out, clreos...), bs...)); err != nil {
					return nil, err
				}
			}
		case actDeleteForwardChar:
			if pos < len(buffer) {
				_, n := utf8.DecodeRune(buffer[pos:])
				copy(buffer[pos:], buffer[pos+n:])
				buffer = buffer[:len(buffer)-n]
				out, bs := transform(buffer[pos:])
				if _, err := w.Write(append(append(out, clreos...), bs...)); err != nil {
					return nil, err
				}
			}
		case actKillLine:
			buffer = buffer[:pos]
			if _, err := io.WriteString(w, clreos); err != nil {
				return nil, err
			}
		case actKillWholeLine:
			_, bs := transform(buffer[:pos])
			if _, err := w.Write(append(bs, clreos...)); err != nil {
				return nil, err
			}
			buffer = buffer[:0]
			pos = 0
		case actRefresh:
			_, bs := transform(buffer[:pos])
			if _, err := w.Write(append(bs, ("\r" + clreos + prompt)...)); err != nil {
				return nil, err
			}
			out, _ := transform(buffer)
			_, bs = transform(buffer[pos:])
			if _, err := w.Write(append(out, bs...)); err != nil {
				return nil, err
			}
		case actPasteStart:
			inPaste = true
		case actPasteEnd:
			inPaste = false
		case actQuotedInsert:
			if len(token) > 2 {
				cp, err := strconv.ParseUint(string(token[2:]), 16, 32)
				if err != nil {
					token = token[1:]
				} else if token[1] == 'x' {
					token = []byte{byte(cp)}
				} else {
					buf := make([]byte, utf8.UTFMax)
					n := utf8.EncodeRune(buf, rune(cp))
					token = buf[:n]
				}
			} else if scanner.Scan() {
				token = scanner.Bytes()
			}
			fallthrough
		case actInsertChar:
			if pos == len(buffer) {
				buffer = append(buffer, token...)
				pos = len(buffer)
				out, _ := transform(token)
				if _, err := w.Write(out); err != nil {
					return nil, err
				}
			} else {
				newlen := len(buffer) + len(token)
				if newlen > cap(buffer) {
					newPassword := make([]byte, 2*newlen)
					copy(newPassword, buffer)
					buffer = newPassword
				}
				buffer = buffer[:newlen]
				copy(buffer[pos+len(token):], buffer[pos:])
				copy(buffer[pos:], token)
				pos += len(token)
				out, _ := transform(token)
				if _, err := w.Write(out); err != nil {
					return nil, err
				}
				out, bs := transform(buffer[pos:])
				if _, err := w.Write(append(append(out, clreos...), bs...)); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buffer, nil

}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}

type readResult struct {
	p   []byte
	err error
}

// ReadRaw reads a line of input from the terminal with custom transform function.
func (t *Terminal) ReadRaw(ctx context.Context, prompt string, transform TransformFunc) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	r := readerFunc(func(p []byte) (int, error) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		ch := make(chan readResult)
		go func() {
			defer close(ch)
			b := make([]byte, len(p))
			n, err := t.ReadContext(ctx, b)
			select {
			case <-ctx.Done():
			case ch <- readResult{p: b[:n], err: err}:
			}
		}()

		select {
		case sig := <-signalCh:
			if ssig, ok := sig.(syscall.Signal); ok {
				return 0, SignalError(ssig)
			}
			return 0, errors.New("caught signal: " + sig.String())
		case <-ctx.Done():
			return 0, ctx.Err()
		case result := <-ch:
			return copy(p, result.p), result.err
		}
	})

	err := t.MakeRaw()
	if err != nil {
		return nil, err
	}
	defer t.Restore()

	return readLine(r, t, prompt, transform)
}

// ReadString reads a line of input from the terminal.
func (t *Terminal) ReadString(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadRaw(ctx, prompt, CaretNotation)
}

// ReadPassword reads a line of input from the terminal with masking.
func (t *Terminal) ReadPassword(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadRaw(ctx, prompt, Masked)
}

// ReadNoEcho reads a line of input from the terminal without local echo.
func (t *Terminal) ReadNoEcho(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadRaw(ctx, prompt, Blanked)
}
