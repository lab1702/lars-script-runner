//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setPlatformProcessAttrs sets platform-specific process attributes for Windows
func setPlatformProcessAttrs(cmd *exec.Cmd) {
	// Windows: Create new process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// getProcessGroupID returns the process group ID for a given PID (not supported on Windows)
func getProcessGroupID(pid int) (int, error) {
	return 0, fmt.Errorf("process groups not supported on Windows")
}

// killProcessGroup kills a process group by PGID (not supported on Windows)
func killProcessGroup(pgid int) error {
	return fmt.Errorf("process groups not supported on Windows")
}