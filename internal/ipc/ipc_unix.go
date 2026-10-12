//go:build darwin || linux

package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"d2l-cli/internal/native"
)

const maxFrame = 64 << 10

var ErrUnavailable = errors.New("browser bridge is disconnected")
var ErrBridgeInUse = errors.New("another bridge host is already running")

type Handler func(context.Context, native.Request) (native.Response, error)

func SocketPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "", errors.New("user configuration directory is unavailable")
	}
	return filepath.Join(base, "d2l-cli", "bridge.sock"), nil
}

func Serve(ctx context.Context, path string, handler Handler) error {
	listener, err := listenSecure(path)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	slots := make(chan struct{}, 32)
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				serveConn(ctx, conn, handler)
			}()
		default:
			conn.Close()
		}
	}
}

func Call(ctx context.Context, path string, request native.Request) (native.Response, error) {
	if err := native.ValidateRequest(request); err != nil {
		return native.Response{}, err
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return native.Response{}, ErrUnavailable
	}
	defer conn.Close()
	deadline := time.Now().Add(20 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	if err := writeFrame(conn, request); err != nil {
		return native.Response{}, ErrUnavailable
	}
	var response native.Response
	if err := readFrame(conn, &response); err != nil {
		return native.Response{}, ErrUnavailable
	}
	if response.ID != request.ID {
		return native.Response{}, errors.New("bridge returned a mismatched response")
	}
	body, err := json.Marshal(response)
	if err != nil {
		return native.Response{}, errors.New("bridge returned an invalid response")
	}
	return native.DecodeExtensionResponse(body)
}

func serveConn(ctx context.Context, conn net.Conn, handler Handler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	var request native.Request
	if err := readFrame(conn, &request); err != nil {
		return
	}
	if err := native.ValidateRequest(request); err != nil {
		_ = writeFrame(conn, native.Response{ID: request.ID, OK: false, Error: &native.ResponseError{Code: "invalid_request", Message: "Request was rejected."}})
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, err := handler(requestCtx, request)
	if err != nil {
		response = native.Response{ID: request.ID, OK: false, Error: &native.ResponseError{Code: "disconnected", Message: "Browser extension is disconnected."}}
	}
	_ = writeFrame(conn, response)
}

func listenSecure(path string) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create private bridge directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("bridge directory must be a current-user-only directory")
	}
	if !ownedByCurrentUser(info) {
		return nil, errors.New("bridge directory is not owned by the current user")
	}
	if existing, err := os.Lstat(path); err == nil {
		if existing.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(existing) {
			return nil, errors.New("refusing to replace an unexpected bridge path")
		}
		conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil, ErrBridgeInUse
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale bridge socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, err
	}
	return listener, nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func writeFrame(w io.Writer, message any) error {
	body, err := json.Marshal(message)
	if err != nil || len(body) == 0 || len(body) > maxFrame {
		return errors.New("invalid local bridge message")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, body)
}

func readFrame(r io.Reader, out any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > maxFrame {
		return errors.New("invalid local bridge frame size")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple local bridge JSON values")
	}
	return nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
