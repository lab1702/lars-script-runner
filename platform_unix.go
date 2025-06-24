//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setPlatformProcessAttrs sets platform-specific process attributes for Unix/Linux/macOS
func setPlatformProcessAttrs(cmd *exec.Cmd) {
	// Unix/Linux/macOS: Set process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}
}

// getProcessGroupID returns the process group ID for a given PID
func getProcessGroupID(pid int) (int, error) {
	return syscall.Getpgid(pid)
}

// killProcessGroup kills a process group by PGID
func killProcessGroup(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}