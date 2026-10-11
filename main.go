package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"d2l-cli/internal/bridge"
	"d2l-cli/internal/ipc"
	"d2l-cli/internal/native"
)

// expectedExtensionOrigin is set by the Chrome host installer at build time.
var expectedExtensionOrigin string

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "native-host" || strings.HasPrefix(args[0], "chrome-extension://")) {
		if expectedExtensionOrigin == "" {
			fmt.Fprintln(os.Stderr, "native-host is not registered for a specific extension ID")
			os.Exit(2)
		}
		if err := runNativeBridgeHost(nativeHostArgs(args), os.Stdin, os.Stdout, os.Stderr, expectedExtensionOrigin, ""); err != nil {
			fmt.Fprintln(os.Stderr, "native-host: request rejected or browser disconnected")
			os.Exit(1)
		}
		return
	}
	if err := runCLI(args, os.Stdout, os.Stderr, ""); err != nil {
		os.Exit(1)
	}
}

func nativeHostArgs(args []string) []string {
	// Chrome launches the registered executable directly and supplies the
	// extension origin as argv[1]. The explicit subcommand is also accepted
	// for manual protocol testing.
	if len(args) > 0 && args[0] == "native-host" {
		return args[1:]
	}
	return args
}

func runNativeHost(args []string, in io.Reader, out, diagnostics io.Writer, expectedOrigin string) error {
	if err := validateNativeHostArgs(args, diagnostics, expectedOrigin); err != nil {
		return err
	}
	return native.Serve(in, out)
}

func runNativeBridgeHost(args []string, in io.Reader, out, diagnostics io.Writer, expectedOrigin, socketPath string) error {
	if err := validateNativeHostArgs(args, diagnostics, expectedOrigin); err != nil {
		return err
	}
	if socketPath == "" {
		var err error
		socketPath, err = ipc.SocketPath()
		if err != nil {
			return err
		}
	}
	return bridge.Serve(context.Background(), in, out, socketPath)
}

func validateNativeHostArgs(args []string, diagnostics io.Writer, expectedOrigin string) error {
	if len(args) == 0 || args[0] != expectedOrigin || !native.IsExtensionOrigin(expectedOrigin) {
		fmt.Fprintln(diagnostics, "native-host: caller origin is not allowlisted")
		return native.ErrUnauthorizedOrigin
	}
	if len(args) > 1 {
		// Chrome on Windows may append --parent-window. It is not used by this host.
		for _, arg := range args[1:] {
			if len(arg) < len("--parent-window=") || arg[:len("--parent-window=")] != "--parent-window=" {
				fmt.Fprintln(diagnostics, "native-host: unexpected launch argument")
				return native.ErrUnauthorizedOrigin
			}
		}
	}
	return nil
}

var requestCounter atomic.Uint64

func newRequestID() string {
	return fmt.Sprintf("cli-%d-%d", time.Now().UnixNano(), requestCounter.Add(1))
}

func runCLI(args []string, stdout, stderr io.Writer, socketPath string) error {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(stdout, "Usage: d2l <status|whoami>")
		return nil
	}
	if len(args) != 1 || (args[0] != "status" && args[0] != "whoami") {
		fmt.Fprintln(stderr, "Usage: d2l <status|whoami>")
		return fmt.Errorf("unsupported command")
	}
	if socketPath == "" {
		var err error
		socketPath, err = ipc.SocketPath()
		if err != nil {
			fmt.Fprintln(stderr, "Browser bridge is disconnected. Open Brightspace in Chrome or Edge and try again.")
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
	defer cancel()
	request := native.Request{ID: newRequestID(), Operation: "ping", Params: []byte(`{}`)}
	if args[0] == "whoami" {
		request.Operation = "whoami"
	}
	response, err := ipc.Call(ctx, socketPath, request)
	if err != nil || !response.OK {
		message := "Browser bridge is disconnected. Open Brightspace in Chrome or Edge and try again."
		if response.Error != nil && response.Error.Code == "auth_required" {
			message = "Brightspace sign-in is required. Sign in in the browser and retry."
		}
		if args[0] == "status" {
			fmt.Fprintln(stdout, "Browser bridge: disconnected")
			return nil
		}
		fmt.Fprintln(stderr, message)
		return fmt.Errorf("browser bridge request failed")
	}
	if args[0] == "status" {
		if response.Data == nil || response.Data.Status != "ready" {
			fmt.Fprintln(stdout, "Browser bridge: disconnected")
			return nil
		}
		fmt.Fprintln(stdout, "Browser bridge: connected")
		return nil
	}
	if response.Data == nil || response.Data.Identifier == "" || response.Data.UniqueName == "" {
		fmt.Fprintln(stderr, "Browser returned an incomplete whoami response.")
		return fmt.Errorf("invalid whoami response")
	}
	fmt.Fprintf(stdout, "Identifier: %s\nUnique name: %s\n", response.Data.Identifier, response.Data.UniqueName)
	return nil
}
