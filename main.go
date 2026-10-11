package main

import (
	"fmt"
	"io"
	"os"

	"d2l-cli/internal/native"
)

// expectedExtensionOrigin is set by the Chrome host installer at build time.
var expectedExtensionOrigin string

func main() {
	if expectedExtensionOrigin == "" {
		fmt.Fprintln(os.Stderr, "native-host is not registered for a specific extension ID")
		os.Exit(2)
	}
	args := nativeHostArgs(os.Args[1:])
	if err := runNativeHost(args, os.Stdin, os.Stdout, os.Stderr, expectedExtensionOrigin); err != nil {
		fmt.Fprintln(os.Stderr, "native-host: request rejected or stream closed")
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
	return native.Serve(in, out)
}
