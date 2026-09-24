// Package desktop holds the small helpers the Wails desktop shell needs to
// resolve portable paths and the loopback address of the embedded server.
package desktop

import (
	"net"
	"os"
	"path/filepath"
)

// DefaultListen is the fixed loopback address of the desktop server. It is
// fixed rather than random because PublicURL is baked into generated STRM files
// at startup, so a drifting port would invalidate existing STRM links.
const DefaultListen = "127.0.0.1:18765"

// ExecutableDir returns the directory that contains the running executable.
func ExecutableDir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(executable), nil
}

// DefaultDataDir returns the portable data directory next to the executable.
func DefaultDataDir() (string, error) {
	dir, err := ExecutableDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "data"), nil
}

// LocalURL turns a listen address into a browser URL, mapping wildcard hosts to
// the loopback interface so the WebView always loads a reachable address.
func LocalURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
