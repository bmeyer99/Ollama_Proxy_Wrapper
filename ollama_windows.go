//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// configureCommand sets Windows-specific attributes for exec.Cmd
func configureCommand(cmd *exec.Cmd) {
	// Set process attributes to ensure child process is killed when parent dies
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NEW_PROCESS_GROUP flag
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		// Hide window for service mode
		HideWindow: IsRunningAsService(),
	}
}