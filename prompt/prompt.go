// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

package prompt

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/text/width"
)

// SignalError indicates that the operation was interrupted by a signal.
type SignalError syscall.Signal

func (e SignalError) Error() string {
	return fmt.Sprintf("recerived signal %v", syscall.Signal(e))
}

// Signal returns the signal number.
func (e SignalError) Signal() int {
	return int(e)
}

// Transformer transforms src into its display form and a sequence of backspaces for deleting it.
type Transformer func(src []byte) (disp, bs []byte)

// CaretNotation displays control characters in caret notation.
func CaretNotation(src []byte) ([]byte, []byte) {
	disp := make([]byte, 0, len(src))
	n := 0

	for len(src) > 0 {
		if b := src[0]; isctrl(b) {
			disp = append(disp, '^', byte(b)^0x40)
			n += 2
			src = src[1:]
		} else if b < utf8.RuneSelf {
			disp = append(disp, b)
			n += 1
			src = src[1:]
		} else {
			r, size := utf8.DecodeRune(src)
			disp = append(disp, src[:size]...)
			switch width.LookupRune(r).Kind() {
			case width.EastAsianWide, width.EastAsianFullwidth:
				n += 2
			default:
				n += 1
			}
			src = src[size:]
		}
	}

	return disp, bytes.Repeat([]byte{'\b'}, n)
}

// Masked displays all characters as asterisks.
func Masked(src []byte) ([]byte, []byte) {
	n := utf8.RuneCount(src)
	return bytes.Repeat([]byte{'*'}, n), bytes.Repeat([]byte{'\b'}, n)
}

// Blanked displays nothing.
func Blanked(src []byte) ([]byte, []byte) {
	return []byte{}, []byte{}
}

type action int

const (
	actInsertChar action = iota
	actQuotedInsert
	actIgnore
	actAccept
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
	actRefresh
	actPasteStart
	actPasteEnd
)

func scanToken(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	switch data[0] {
	case 0x16: // ^V
		if len(data) == 1 {
			if atEOF {
				return 1, data[:1], nil
			} else {
				return 0, nil, nil
			}
		}

		var maxlen int
		switch data[1] {
		case 'x':
			maxlen = 4
		case 'u':
			maxlen = 6
		case 'U':
			maxlen = 10
		default:
			return 1, data[:1], nil
		}

		i := 2
		for i < len(data) && i < maxlen && ishex(data[i]) {
			i++
		}
		if i == len(data) && i < maxlen && !atEOF {
			return 0, nil, nil
		} else if i == 2 {
			return 1, data[:1], nil
		}
		return i, data[:i], nil
	case 0x1b: // ^[
		if len(data) >= 3 && data[1] == '[' {
			i := 2
			for i < len(data) && data[i]-0x30 <= 0x0f {
				i++
			}
			for i < len(data) && data[i]-0x20 <= 0x0f {
				i++
			}
			if i < len(data) && data[i]-0x40 <= 0x3e {
				i++
				return i, data[:i], nil
			}
		} else if len(data) >= 3 && data[1] == 'O' && data[2]-0x20 <= 0x5f {
			return 3, data[:3], nil
		}
		return 1, data[:1], nil
	default:
		_, n := utf8.DecodeRune(data)
		return n, data[:n], nil
	}
}

func tokenToAction(token []byte, inPaste bool) action {
	if inPaste {
		switch asstr(token) {
		case "\x1b[201~": // Bracketed Paste End
			return actPasteEnd
		default:
			return actInsertChar
		}
	}

	if !isctrl(token[0]) {
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
		return actAccept
	case 0x05: // ^E
		return actEndOfLine
	case 0x06: // ^F
		return actForwardChar
	case 0x08: // ^H
		return actDeleteBackwardChar
	case 0x09: // ^I, Tab
		return actInsertChar
	case 0x0a: // ^J, Enter
		return actAccept
	case 0x0b: // ^K
		return actKillLine
	case 0x0c: // ^L
		return actRefresh
	case 0x0d: // ^M
		return actAccept
	case 0x15: // ^U
		return actKillWholeLine
	case 0x16: // ^V
		return actQuotedInsert
	case 0x1b: // ^[, Esc
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

	switch asstr(token) {
	case "\x1b[A", "\x1bOA": // Up
		return actIgnore
	case "\x1b[B", "\x1bOB": // Down
		return actIgnore
	case "\x1b[C", "\x1bOC": // Right
		return actForwardChar
	case "\x1b[D", "\x1bOD": // Left
		return actBackwardChar
	case "\x1b[1~", "\x1b[7~", "\x1b[H", "\x1bOH": // Home
		return actBeginningOfLine
	case "\x1b[2~": // Insert
		return actIgnore
	case "\x1b[3~": // Delete
		return actDeleteForwardChar
	case "\x1b[4~", "\x1b[8~", "\x1b[F", "\x1bOF": // End
		return actEndOfLine
	case "\x1b[5~": // Page Up
		return actIgnore
	case "\x1b[6~": // Page Down
		return actIgnore
	case "\x1bOM": // Enter
		return actAccept
	case "\x1b[200~": // Bracketed Paste Start
		return actPasteStart
	default:
		return actIgnore
	}
}

func (t *Terminal) readLine(r io.Reader, prompt string, transform Transformer) (s []byte, err error) {
	buffer := make([]byte, 0, 256)
	cursor := 0
	inPaste := false

	if _, err2 := t.Write(concat("\r", clreos, prompt, ewrap, ebp)); err2 != nil {
		return nil, err2
	}

	defer func() {
		disp, _ := transform(buffer[cursor:])
		if _, err2 := t.Write(concat(disp, "\r\n", dbp)); err2 != nil {
			err = errors.Join(err, err2)
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Split(scanToken)
	for scanner.Scan() {
		token := scanner.Bytes()
		var output []byte
		switch action := tokenToAction(token, inPaste); action {
		case actAccept:
			return buffer, nil
		case actSIGINT:
			return nil, SignalError(syscall.SIGINT)
		case actSIGQUIT:
			return nil, SignalError(syscall.SIGQUIT)
		case actBeginningOfLine:
			if cursor > 0 {
				_, output = transform(buffer[:cursor])
				cursor = 0
			}
		case actEndOfLine:
			if cursor < len(buffer) {
				output, _ = transform(buffer[cursor:])
				cursor = len(buffer)
			}
		case actBackwardChar:
			if cursor > 0 {
				_, n := utf8.DecodeLastRune(buffer[:cursor])
				_, output = transform(buffer[cursor-n : cursor])
				cursor -= n
			}
		case actForwardChar:
			if cursor < len(buffer) {
				_, n := utf8.DecodeRune(buffer[cursor:])
				output, _ = transform(buffer[cursor : cursor+n])
				cursor += n
			}
		case actDeleteBackwardChar:
			if cursor > 0 {
				_, n := utf8.DecodeLastRune(buffer[:cursor])
				_, bs := transform(buffer[cursor-n : cursor])
				buffer = slices.Delete(buffer, cursor-n, cursor)
				cursor -= n
				if cursor < len(buffer) {
					disp, _ := transform(buffer[cursor:])
					output = concat(bs, clreos, sc, disp, rc)
				} else {
					output = concat(bs, clreos)
				}
			}
		case actDeleteForwardChar:
			if cursor < len(buffer) {
				_, n := utf8.DecodeRune(buffer[cursor:])
				buffer = slices.Delete(buffer, cursor, cursor+n)
				disp, _ := transform(buffer[cursor:])
				output = concat(clreos, sc, disp, rc)
			}
		case actKillLine:
			buffer = buffer[:cursor]
			output = concat(clreos)
		case actKillWholeLine:
			_, bs := transform(buffer[:cursor])
			buffer = buffer[:0]
			cursor = 0
			output = concat(bs, "\r", clreos, prompt)
		case actRefresh:
			disp1, bs := transform(buffer[:cursor])
			disp2, _ := transform(buffer[cursor:])
			output = concat(bs, "\r", clreos, prompt, disp1, sc, disp2, rc)
		case actPasteStart:
			inPaste = true
		case actPasteEnd:
			inPaste = false
		case actQuotedInsert:
			if len(token) > 1 {
				n, err := strconv.ParseUint(asstr(token[2:]), 16, 32)
				if err != nil {
					token = token[1:]
				} else if token[1] == 'x' {
					token = append(token[:0], byte(n))
				} else {
					token = utf8.AppendRune(token[:0], rune(n))
				}
			} else if scanner.Scan() {
				token = scanner.Bytes()
			}
			fallthrough
		case actInsertChar:
			buffer = slices.Insert(buffer, cursor, token...)
			cursor += len(token)
			disp1, _ := transform(token)
			if cursor < len(buffer) {
				disp2, _ := transform(buffer[cursor:])
				output = concat(disp1, clreos, sc, disp2, rc)
			} else {
				output = concat(disp1)
			}
		}
		if _, err2 := t.Write(output); err2 != nil {
			return nil, err2
		}
	}
	if err2 := scanner.Err(); err2 != nil {
		return nil, err2
	}
	return buffer, nil
}

// ReadCustom reads a line from the terminal with custom transform function.
func (t *Terminal) ReadCustom(ctx context.Context, prompt string, transform Transformer) (s []byte, err error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-signalCh:
			if ssig, ok := sig.(syscall.Signal); ok {
				cancel(SignalError(ssig))
			} else {
				cancel(fmt.Errorf("caught signal: %v", sig))
			}
		case <-ctx.Done():
		}
		signal.Stop(signalCh)
	}()

	r, err2 := t.ContextReader(ctx)
	if err2 != nil {
		return nil, err2
	}
	defer func() {
		err = errors.Join(err, r.Close())
	}()

	if err2 := t.MakeRaw(); err2 != nil {
		return nil, fmt.Errorf("failed to put the terminal into raw mode: %w", err2)
	}
	defer func() {
		if err2 := t.Restore(); err2 != nil {
			err2 = fmt.Errorf("failed to restore the terminal from raw mode: %w", err2)
			err = errors.Join(err, err2)
		}
	}()

	return t.readLine(r, prompt, transform)
}

// ReadLine reads a line from the terminal.
func (t *Terminal) ReadLine(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, CaretNotation)
}

// ReadPassword reads a line from the terminal with masking.
func (t *Terminal) ReadPassword(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, Masked)
}

// ReadNoEcho reads a line from the terminal without local echo.
func (t *Terminal) ReadNoEcho(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, Blanked)
}

func isctrl(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func ishex(b byte) bool {
	return b-0x30 < 10 || ((b&^0x20)-0x41) < 6
}

func asstr(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func concat(a ...any) []byte {
	n := 0
	for _, x := range a {
		switch x := x.(type) {
		case []byte:
			n += len(x)
		case string:
			n += len(x)
		}
		if n < 0 {
			panic("concat: output length overflow")
		}
	}
	s := make([]byte, 0, n)
	for _, x := range a {
		switch x := x.(type) {
		case []byte:
			s = append(s, x...)
		case string:
			s = append(s, x...)
		}
	}
	return s
}
