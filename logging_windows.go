//go:build windows

package main

import (
	"fmt"
	"log"
)

// ServiceLogger is the logger instance for Windows service mode
var ServiceLogger *log.Logger

// LogPrintf logs messages with appropriate destination based on running mode
func LogPrintf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if ServiceLogger != nil {
		ServiceLogger.Println(msg)
	} else {
		log.Println(msg)
	}
}

// getCurrentDirectory returns the current working directory
func getCurrentDirectory() string {
	return getCurrentWorkingDir()
}