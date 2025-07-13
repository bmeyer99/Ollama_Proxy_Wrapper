//go:build !windows

package main

// runAsService is a stub for non-Windows platforms
func runAsService() {
	// This function is only implemented on Windows
	panic("Service mode is only supported on Windows")
}

// IsRunningAsService returns false on non-Windows platforms
func IsRunningAsService() bool {
	return false
}