package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	// Chrome currently accepts at most 1 MiB from the host and allows up to
	// 64 MiB in the other direction. The application uses a much smaller cap.
	ChromeMaxHostMessageBytes = 1 << 20
	ChromeMaxRequestBytes     = 64 << 20
	MaxApplicationMessage     = 64 << 10
	MaxApplicationResponse    = 64 << 10
)

var (
	ErrFrameTooLarge      = errors.New("native message exceeds size limit")
	ErrInvalidFrame       = errors.New("invalid native message frame")
	ErrUnauthorizedOrigin = errors.New("unauthorized extension origin")
)

// ReadFrame reads a 4-byte native-endian length followed by UTF-8 bytes.
// It rejects an oversized length before allocating or reading the body.
func ReadFrame(r io.Reader, maxBytes uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.NativeEndian.Uint32(header[:])
	if n == 0 {
		return nil, ErrInvalidFrame
	}
	if n > maxBytes {
		return nil, ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("%w: truncated body", ErrInvalidFrame)
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("%w: body is not UTF-8", ErrInvalidFrame)
	}
	return body, nil
}

// WriteFrame writes one UTF-8 JSON frame in native byte order.
func WriteFrame(w io.Writer, body []byte, maxBytes uint32) error {
	if len(body) == 0 {
		return ErrInvalidFrame
	}
	if uint64(len(body)) > uint64(maxBytes) || uint64(len(body)) > uint64(^uint32(0)) {
		return ErrFrameTooLarge
	}
	if !utf8.Valid(body) {
		return fmt.Errorf("%w: body is not UTF-8", ErrInvalidFrame)
	}
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], uint32(len(body)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, body)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
