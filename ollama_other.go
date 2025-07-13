//go:build !windows

package main

import "os/exec"

// configureCommand is a no-op on non-Windows platforms
func configureCommand(cmd *exec.Cmd) {
	// No special configuration needed on non-Windows platforms
}