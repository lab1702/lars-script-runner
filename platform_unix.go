//go:build !windows

package main

import (
	"log/slog"
	"os"
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

// sendPlatformTerminationSignal sends platform-specific termination signal
func sendPlatformTerminationSignal(proc *os.Process, cmd string) error {
	// Unix/Linux/macOS: Send SIGTERM to process group for graceful shutdown
	if pgid, err := getProcessGroupID(proc.Pid); err == nil {
		// Send SIGTERM to process group (negative PID)
		err := syscall.Kill(-pgid, syscall.SIGTERM)
		if err != nil {
			slog.Warn("failed_to_send_sigterm_to_group", "process", cmd, "pgid", pgid, "error", err)
			// Fallback to single process
			err = proc.Signal(syscall.SIGTERM)
			if err != nil {
				slog.Warn("failed_to_send_sigterm", "process", cmd, "pid", proc.Pid, "error", err)
			}
			return err
		}
		slog.Debug("sent_sigterm_to_process_group", "process", cmd, "pgid", pgid)
		return nil
	} else {
		// Fallback to single process if we can't get process group
		slog.Debug("no_process_group_sending_sigterm_to_process", "process", cmd, "pid", proc.Pid)
		err := proc.Signal(syscall.SIGTERM)
		if err != nil {
			slog.Warn("failed_to_send_sigterm", "process", cmd, "pid", proc.Pid, "error", err)
		}
		return err
	}
}

