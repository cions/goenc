// Copyright (c) 2020-2024 cions
// Licensed under the MIT License. See LICENSE for details.

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
	"slices"
	"strconv"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/text/width"
)

const (
	clreos         = "\x1b[J"      // Clear to end of screen
	save_cursor    = "\x1b[s"      // Save cursor
	restore_cursor = "\x1b[u"      // Restore cursor
	ebp            = "\x1b[?2004h" // Enable Bracketed Paste Mode
	dbp            = "\x1b[?2004l" // Disable Bracketed Paste Mode
)

// SignalError indicates that the operation was interrupted by a signal.
type SignalError syscall.Signal

func (se SignalError) Error() string {
	return "received signal " + syscall.Signal(se).String()
}

// Signal returns the signal number.
func (se SignalError) Signal() int {
	return int(se)
}

// TransformFunc transforms src into its display form and a sequence of backspaces for deleting it.
type TransformFunc func(src []byte) (disp, bs []byte)

// CaretNotation displays control characters in caret notation.
func CaretNotation(src []byte) ([]byte, []byte) {
	disp := make([]byte, 0, len(src))
	n := 0

	for len(src) > 0 {
		r, size := utf8.DecodeRune(src)
		if r < 0x20 || r == 0x7f {
			disp = append(disp, '^', byte(r)^0x40)
			n += 2
		} else {
			disp = append(disp, src[:size]...)
			switch p, _ := width.Lookup(src); p.Kind() {
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

func isHex(b byte) bool {
	return b-0x30 < 10 || ((b&0xdf)-0x41) < 6
}

func bytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func concat(elems ...any) []byte {
	ret := []byte(nil)
	for _, x := range elems {
		switch x := x.(type) {
		case []byte:
			ret = append(ret, x...)
		case string:
			ret = append(ret, x...)
		}
	}
	return ret
}

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
		for i < len(data) && i < maxlen && isHex(data[i]) {
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
		switch bytesToString(token) {
		case "\x1b[201~": // Bracketed Paste End
			return actPasteEnd
		default:
			return actInsertChar
		}
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

	switch bytesToString(token) {
	case "\x1b[C", "\x1bOC": // Right
		return actForwardChar
	case "\x1b[D", "\x1bOD": // Left
		return actBackwardChar
	case "\x1b[1~", "\x1b[7~", "\x1b[H", "\x1bOH": // Home
		return actBeginningOfLine
	case "\x1b[4~", "\x1b[8~", "\x1b[F", "\x1bOF": // End
		return actEndOfLine
	case "\x1b[3~": // Delete
		return actDeleteForwardChar
	case "\x1bOM": // Enter
		return actAccept
	case "\x1b[200~": // Bracketed Paste Start
		return actPasteStart
	default:
		return actIgnore
	}
}

func (t *Terminal) readLine(r io.Reader, prompt string, transform TransformFunc) ([]byte, error) {
	buffer := make([]byte, 0, 256)
	cursor := 0
	inPaste := false

	if _, err := t.Write(concat("\r", clreos, prompt, ebp)); err != nil {
		return nil, err
	}

	defer func() {
		disp, _ := transform(buffer[cursor:])
		t.Write(concat(disp, "\r\n", dbp))
	}()

	scanner := bufio.NewScanner(r)
	scanner.Split(scanToken)
	for scanner.Scan() {
		token := scanner.Bytes()
		action := tokenToAction(token, inPaste)
		var out []byte
		switch action {
		case actAccept:
			return buffer, nil
		case actSIGINT:
			return nil, SignalError(syscall.SIGINT)
		case actSIGQUIT:
			return nil, SignalError(syscall.SIGQUIT)
		case actBeginningOfLine:
			if cursor > 0 {
				_, out = transform(buffer[:cursor])
				cursor = 0
			}
		case actEndOfLine:
			if cursor < len(buffer) {
				out, _ = transform(buffer[cursor:])
				cursor = len(buffer)
			}
		case actBackwardChar:
			if cursor > 0 {
				_, n := utf8.DecodeLastRune(buffer[:cursor])
				_, out = transform(buffer[cursor-n : cursor])
				cursor -= n
			}
		case actForwardChar:
			if cursor < len(buffer) {
				_, n := utf8.DecodeRune(buffer[cursor:])
				out, _ = transform(buffer[cursor : cursor+n])
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
					out = concat(bs, clreos, save_cursor, disp, restore_cursor)
				} else {
					out = concat(bs, clreos)
				}
			}
		case actDeleteForwardChar:
			if cursor < len(buffer) {
				_, n := utf8.DecodeRune(buffer[cursor:])
				buffer = slices.Delete(buffer, cursor, cursor+n)
				disp, _ := transform(buffer[cursor:])
				out = concat(clreos, save_cursor, disp, restore_cursor)
			}
		case actKillLine:
			buffer = buffer[:cursor]
			out = concat(clreos)
		case actKillWholeLine:
			_, bs := transform(buffer[:cursor])
			buffer = buffer[:0]
			cursor = 0
			out = concat(bs, "\r", clreos, prompt)
		case actRefresh:
			disp1, bs := transform(buffer[:cursor])
			disp2, _ := transform(buffer[cursor:])
			out = concat(bs, "\r", clreos, prompt, disp1, save_cursor, disp2, restore_cursor)
		case actPasteStart:
			inPaste = true
		case actPasteEnd:
			inPaste = false
		case actQuotedInsert:
			if len(token) > 1 {
				cp, err := strconv.ParseUint(bytesToString(token[2:]), 16, 32)
				if err != nil {
					token = token[1:]
				} else if token[1] == 'x' {
					token = []byte{byte(cp)}
				} else {
					token = utf8.AppendRune(nil, rune(cp))
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
				out = concat(clreos, disp1, save_cursor, disp2, restore_cursor)
			} else {
				out = concat(disp1)
			}
		}
		if _, err := t.Write(out); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buffer, nil
}

// ReadCustom reads a line of input from the terminal with custom transform function.
func (t *Terminal) ReadCustom(ctx context.Context, prompt string, transform TransformFunc) ([]byte, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-signalCh:
			if ssig, ok := sig.(syscall.Signal); ok {
				cancel(SignalError(ssig))
			}
			cancel(errors.New("caught signal: " + sig.String()))
		case <-ctx.Done():
		}
		signal.Stop(signalCh)
	}()

	r, err := t.ContextReader(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	if err := t.MakeRaw(); err != nil {
		return nil, err
	}
	defer t.Restore()

	return t.readLine(r, prompt, transform)
}

// ReadLine reads a line of input from the terminal.
func (t *Terminal) ReadLine(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, CaretNotation)
}

// ReadPassword reads a line of input from the terminal with masking.
func (t *Terminal) ReadPassword(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, Masked)
}

// ReadNoEcho reads a line of input from the terminal without local echo.
func (t *Terminal) ReadNoEcho(ctx context.Context, prompt string) ([]byte, error) {
	return t.ReadCustom(ctx, prompt, Blanked)
}
