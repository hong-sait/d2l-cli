//go:build windows

package ipc

import (
	"context"
	"errors"

	"d2l-cli/internal/native"
)

var ErrUnavailable = errors.New("browser bridge is disconnected or unsupported on this platform")

type Handler func(context.Context, native.Request) (native.Response, error)

func SocketPath() (string, error)                  { return "", ErrUnavailable }
func Serve(context.Context, string, Handler) error { return ErrUnavailable }
func Call(context.Context, string, native.Request) (native.Response, error) {
	return native.Response{}, ErrUnavailable
}
