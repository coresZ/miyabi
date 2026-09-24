//go:build !windows || !miyabidesktop

// Package main is the Wails desktop entry point for Miyabi. The real
// implementation lives in main.go behind the `windows && miyabidesktop` build
// constraint; this stub keeps `go build ./...` and `go test ./...` working on
// machines that do not have the Wails module available.
package main

import "fmt"

func main() {
	fmt.Println("miyabi-desktop requires a Windows build with the miyabidesktop build tag; see DESKTOP_PLAN.md")
}
