//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
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

// sendPlatformTerminationSignal sends platform-specific termination signal
func sendPlatformTerminationSignal(proc *os.Process, cmd string) error {
	// Windows: Check if process is still running before attempting termination
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return nil // Process already terminated
	}

	// For PowerShell processes on Windows, force kill immediately
	// as they don't handle graceful termination well
	if strings.Contains(strings.ToLower(cmd), "powershell") || strings.Contains(strings.ToLower(cmd), "pwsh") {
		slog.Debug("powershell_force_kill_on_windows", "process", cmd, "pid", proc.Pid)
		return fmt.Errorf("force killed powershell process")
	}

	// For other Windows processes, use timeout-based termination
	slog.Debug("windows_timeout_based_termination", "process", cmd, "pid", proc.Pid)
	return nil
}

