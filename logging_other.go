//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
)

// ServiceLogger is not used on non-Windows platforms
var ServiceLogger *log.Logger

// LogPrintf logs messages (non-Windows version)
func LogPrintf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

// getCurrentDirectory returns the current working directory
func getCurrentDirectory() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "unknown"
}