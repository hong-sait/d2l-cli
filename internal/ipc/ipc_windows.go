//go:build windows

package ipc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"d2l-cli/internal/native"
)

const maxFrame = 64 << 10

var ErrUnavailable = errors.New("browser bridge is disconnected")
var ErrBridgeInUse = errors.New("another bridge host is already running")

type Handler func(context.Context, native.Request) (native.Response, error)

const (
	pipeAccessDuplex       = 0x00000003
	pipeTypeByte           = 0x00000000
	pipeReadModeByte       = 0x00000000
	pipeWait               = 0x00000000
	pipeRejectRemoteClient = 0x00000008
	pipeUnlimitedInstances = 255
	genericRead            = 0x80000000
	genericWrite           = 0x40000000
	openExisting           = 3
	fileAttributeNormal    = 0x00000080
	errorAlreadyExists     = syscall.Errno(183)
	errorPipeConnected     = syscall.Errno(535)
	errorPipeBusy          = syscall.Errno(231)
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	createNamedPipeProc  = kernel32.NewProc("CreateNamedPipeW")
	connectNamedPipeProc = kernel32.NewProc("ConnectNamedPipe")
	createFileProc       = kernel32.NewProc("CreateFileW")
	waitNamedPipeProc    = kernel32.NewProc("WaitNamedPipeW")
	createMutexProc      = kernel32.NewProc("CreateMutexW")
)

func SocketPath() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil || config == "" {
		return "", errors.New("user configuration directory is unavailable")
	}
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(config))))
	return `\\.\pipe\d2l-cli-` + fmt.Sprintf("%x", hash[:12]), nil
}

func Serve(ctx context.Context, pipeName string, handler Handler) error {
	mutex, err := acquireBridgeMutex(pipeName)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(mutex)

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			wakePipeServer(pipeName, stopped)
		case <-stopped:
		}
	}()
	defer close(stopped)

	slots := make(chan struct{}, 32)
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for {
		pipe, err := createPipe(pipeName)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		err = connectPipe(pipe)
		if err != nil && !errors.Is(err, errorPipeConnected) {
			_ = syscall.CloseHandle(pipe)
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			_ = syscall.CloseHandle(pipe)
			return nil
		}
		file := os.NewFile(uintptr(pipe), "d2l-cli-bridge")
		select {
		case slots <- struct{}{}:
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer func() { <-slots }()
				serveConn(ctx, file, handler)
			}()
		default:
			_ = file.Close()
		}
	}
}

func Call(ctx context.Context, pipeName string, request native.Request) (native.Response, error) {
	if err := native.ValidateRequest(request); err != nil {
		return native.Response{}, err
	}
	pipe, err := openPipe(ctx, pipeName, 2*time.Second)
	if err != nil {
		return native.Response{}, ErrUnavailable
	}
	file := os.NewFile(uintptr(pipe), "d2l-cli-client")
	type callResult struct {
		response native.Response
		err      error
	}
	result := make(chan callResult, 1)
	go func() {
		defer file.Close()
		if err := writeFrame(file, request); err != nil {
			result <- callResult{err: ErrUnavailable}
			return
		}
		var response native.Response
		if err := readFrame(file, &response); err != nil {
			result <- callResult{err: ErrUnavailable}
			return
		}
		if response.ID != request.ID {
			result <- callResult{err: errors.New("bridge returned a mismatched response")}
			return
		}
		body, err := json.Marshal(response)
		if err != nil {
			result <- callResult{err: errors.New("bridge returned an invalid response")}
			return
		}
		decoded, err := native.DecodeExtensionResponse(body)
		result <- callResult{response: decoded, err: err}
	}()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	select {
	case result := <-result:
		return result.response, result.err
	case <-ctx.Done():
		_ = file.Close()
		return native.Response{}, ErrUnavailable
	case <-deadline.C:
		_ = file.Close()
		return native.Response{}, ErrUnavailable
	}
}

func acquireBridgeMutex(pipeName string) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(strings.Replace(pipeName, `\\.\pipe\`, `Local\`, 1) + "-owner")
	if err != nil {
		return 0, err
	}
	handle, _, callErr := createMutexProc.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return 0, callErr
	}
	if errors.Is(callErr, errorAlreadyExists) {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return 0, ErrBridgeInUse
	}
	return syscall.Handle(handle), nil
}

func createPipe(name string) (syscall.Handle, error) {
	pipeName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	handle, _, callErr := createNamedPipeProc.Call(
		uintptr(unsafe.Pointer(pipeName)), pipeAccessDuplex,
		pipeTypeByte|pipeReadModeByte|pipeWait|pipeRejectRemoteClient,
		pipeUnlimitedInstances, maxFrame+4, maxFrame+4, 0, 0)
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return 0, callErr
	}
	return syscall.Handle(handle), nil
}

func connectPipe(pipe syscall.Handle) error {
	result, _, callErr := connectNamedPipeProc.Call(uintptr(pipe), 0)
	if result != 0 {
		return nil
	}
	return callErr
}

func openPipe(ctx context.Context, name string, timeout time.Duration) (syscall.Handle, error) {
	pipeName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, ErrUnavailable
		}
		wait := uint32(remaining.Milliseconds())
		if wait == 0 {
			wait = 1
		}
		ok, _, waitErr := waitNamedPipeProc.Call(uintptr(unsafe.Pointer(pipeName)), uintptr(wait))
		if ok == 0 {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if errors.Is(waitErr, errorPipeBusy) {
				continue
			}
			return 0, waitErr
		}
		handle, _, openErr := createFileProc.Call(
			uintptr(unsafe.Pointer(pipeName)), genericRead|genericWrite, 0, 0,
			openExisting, fileAttributeNormal, 0)
		if syscall.Handle(handle) != syscall.InvalidHandle {
			return syscall.Handle(handle), nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if !errors.Is(openErr, errorPipeBusy) {
			return 0, openErr
		}
	}
}

func wakePipeServer(name string, stopped <-chan struct{}) {
	for {
		select {
		case <-stopped:
			return
		default:
		}
		pipe, err := openPipe(context.Background(), name, 100*time.Millisecond)
		if err == nil {
			_ = syscall.CloseHandle(pipe)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func serveConn(ctx context.Context, conn io.ReadWriteCloser, handler Handler) {
	defer conn.Close()
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
