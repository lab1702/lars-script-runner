// Tiny program to run multiple commands in parallel and restart them if they exit.
// Created by Lars Bernhardsson during Christmas break, 2023.
// License: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Constants for configuration and limits
const (
	// Application version
	Version = "1.0.0"

	// Default configuration values
	DefaultRestartInterval = time.Second
	DefaultGracePeriod     = 5 * time.Second
	MaxCommandLength       = 1000
	MaxBackoffDuration     = 5 * time.Minute
	BackoffMultiplier      = 2.0
	MaxConsecutiveFailures = 10
)

// Config holds configuration for the application
type Config struct {
	CommandFile    string
	GracePeriod    time.Duration
	RestartDelay   time.Duration
	MaxRetries     int
	BackoffEnabled bool
	WebDashboard   bool
	WebPort        int
	WebHost        string
}

// ProcessStatus represents the current status of a process
type ProcessStatus string

const (
	StatusStopped  ProcessStatus = "stopped"
	StatusRunning  ProcessStatus = "running"
	StatusFailed   ProcessStatus = "failed"
	StatusStarting ProcessStatus = "starting"
)

// ProcessStats holds statistics for a process
type ProcessStats struct {
	StartTime    time.Time     `json:"start_time"`
	RestartCount int           `json:"restart_count"`
	FailureCount int           `json:"failure_count"`
	LastFailure  *time.Time    `json:"last_failure,omitempty"`
	Uptime       time.Duration `json:"uptime"`
	Status       ProcessStatus `json:"status"`
	PID          int           `json:"pid,omitempty"`
	LastOutput   string        `json:"last_output,omitempty"`
}

// ProcessManager handles a single command's lifecycle with graceful shutdown
type ProcessManager struct {
	cmd             string
	command         string
	args            []string
	currentProc     *os.Process
	procMutex       sync.RWMutex
	shutdownGrace   time.Duration
	failureCount    int
	lastFailure     time.Time
	backoffDuration time.Duration
	config          *Config
	// Web dashboard monitoring fields
	stats      ProcessStats
	statsMutex sync.RWMutex
	id         string
	dashboard  *DashboardManager
}

// NewProcessManager creates a new process manager with enhanced error handling
func NewProcessManager(cmd string, config *Config, id string, dashboard *DashboardManager) (*ProcessManager, error) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command: %q", cmd)
	}

	return &ProcessManager{
		cmd:             cmd,
		command:         parts[0],
		args:            parts[1:],
		shutdownGrace:   config.GracePeriod,
		failureCount:    0,
		backoffDuration: config.RestartDelay,
		config:          config,
		id:              id,
		dashboard:       dashboard,
		stats: ProcessStats{
			StartTime: time.Now(),
			Status:    StatusStopped,
		},
	}, nil
}

// UpdateStats updates the process statistics
func (pm *ProcessManager) UpdateStats(status ProcessStatus, pid int) {
	pm.statsMutex.Lock()
	defer pm.statsMutex.Unlock()

	pm.stats.Status = status
	pm.stats.PID = pid

	if status == StatusRunning {
		pm.stats.StartTime = time.Now()
	}

	if status == StatusRunning {
		pm.stats.Uptime = time.Since(pm.stats.StartTime)
	}

	if status == StatusFailed {
		pm.stats.FailureCount++
		now := time.Now()
		pm.stats.LastFailure = &now
	}

	if status == StatusStarting {
		pm.stats.RestartCount++
	}

	// Dashboard will update via HTTP polling
}

// GetStats returns a copy of the current process statistics
func (pm *ProcessManager) GetStats() ProcessStats {
	pm.statsMutex.RLock()
	defer pm.statsMutex.RUnlock()

	stats := pm.stats
	if stats.Status == StatusRunning {
		stats.Uptime = time.Since(stats.StartTime)
	}

	return stats
}

// Start begins the process management loop with graceful shutdown support and exponential backoff
func (pm *ProcessManager) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// Initial ticker with configured restart delay
	ticker := time.NewTicker(pm.backoffDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutdown_requested", "process", pm.cmd)
			pm.gracefulShutdown()
			return
		case <-ticker.C:
			// Check if we've exceeded max consecutive failures
			if pm.config.MaxRetries > 0 && pm.failureCount >= pm.config.MaxRetries {
				slog.Error("max_failures_exceeded", "process", pm.cmd, "failures", pm.failureCount)
				return
			}

			if err := pm.startProcess(ctx); err != nil {
				pm.handleProcessFailure(err)
				// Update ticker with new backoff duration
				ticker.Reset(pm.backoffDuration)
			} else {
				// Reset failure count and backoff on successful start
				pm.resetFailureState()
				ticker.Reset(pm.config.RestartDelay)
			}
		}
	}
}

// handleProcessFailure implements exponential backoff for failing processes
func (pm *ProcessManager) handleProcessFailure(err error) {
	pm.failureCount++
	pm.lastFailure = time.Now()

	if pm.config.BackoffEnabled {
		// Exponential backoff with jitter
		backoffMultiplier := math.Pow(BackoffMultiplier, float64(pm.failureCount-1))
		newBackoff := time.Duration(float64(pm.config.RestartDelay) * backoffMultiplier)

		// Cap the backoff duration
		if newBackoff > MaxBackoffDuration {
			newBackoff = MaxBackoffDuration
		}

		pm.backoffDuration = newBackoff
		slog.Warn("process_start_failed_backing_off",
			"process", pm.cmd,
			"error", err,
			"failure_count", pm.failureCount,
			"backoff_duration", pm.backoffDuration)
	} else {
		slog.Warn("process_start_failed", "process", pm.cmd, "error", err, "failure_count", pm.failureCount)
	}
}

// resetFailureState resets failure tracking on successful process start
func (pm *ProcessManager) resetFailureState() {
	if pm.failureCount > 0 {
		slog.Info("process_failure_state_reset", "process", pm.cmd, "previous_failures", pm.failureCount)
		pm.failureCount = 0
		pm.backoffDuration = pm.config.RestartDelay
	}
}

// startProcess starts a single instance of the process
func (pm *ProcessManager) startProcess(ctx context.Context) error {
	slog.Info("starting_process", "process", pm.cmd)

	// Create command with context for cancellation
	cmd := exec.CommandContext(ctx, pm.command, pm.args...)

	// Set platform-specific process attributes
	pm.setPlatformProcessAttrs(cmd)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Store process reference for shutdown with stats update
	pm.procMutex.Lock()
	pm.currentProc = cmd.Process
	pm.procMutex.Unlock()

	// Update stats to reflect running state
	pm.UpdateStats(StatusRunning, cmd.Process.Pid)

	slog.Info("process_started", "process", pm.cmd, "pid", cmd.Process.Pid)

	// Wait for process to complete
	err := cmd.Wait()

	// Clear process reference and update stats atomically
	pm.procMutex.Lock()
	pm.currentProc = nil
	pm.procMutex.Unlock()

	// Update stats based on exit condition
	if err != nil {
		pm.UpdateStats(StatusFailed, 0)
	} else {
		pm.UpdateStats(StatusStopped, 0)
	}

	if err != nil {
		if ctx.Err() != nil {
			slog.Info("process_cancelled", "process", pm.cmd)
		} else {
			slog.Info("process_exited_error", "process", pm.cmd, "error", err)
		}
	} else {
		slog.Info("process_exited_normal", "process", pm.cmd)
	}

	return nil
}

// setPlatformProcessAttrs sets platform-specific process attributes
func (pm *ProcessManager) setPlatformProcessAttrs(cmd *exec.Cmd) {
	setPlatformProcessAttrs(cmd)
}

// terminateProcess attempts to terminate a process gracefully, then forcefully
// Fixed to prevent goroutine leaks and handle PowerShell processes properly
func (pm *ProcessManager) terminateProcess(proc *os.Process) {
	slog.Info("terminating_process", "process", pm.cmd, "pid", proc.Pid)

	// For PowerShell processes, use shorter grace period and more aggressive termination
	gracePeriod := pm.shutdownGrace
	if strings.Contains(strings.ToLower(pm.cmd), "powershell") || strings.Contains(strings.ToLower(pm.cmd), "pwsh") {
		gracePeriod = 2 * time.Second // Shorter grace period for PowerShell
		slog.Debug("using_shorter_grace_period_for_powershell", "process", pm.cmd, "grace_period", gracePeriod)
	}

	// Try graceful shutdown first
	if err := pm.sendTerminationSignal(proc); err != nil {
		slog.Warn("failed_to_send_termination_signal", "process", pm.cmd, "error", err)
		// Fall back to force kill immediately
		pm.forceKillProcess(proc)
		return
	}

	// Create context with timeout to prevent goroutine leaks
	ctx, cancel := context.WithTimeout(context.Background(), gracePeriod)
	defer cancel()

	// Wait for graceful shutdown with proper cleanup
	done := make(chan error, 1)
	go func() {
		defer func() {
			// Always write to channel to prevent goroutine leak
			select {
			case done <- nil:
			default:
				// Channel full, exit
			}
		}()

		// Wait for process to exit
		_, err := proc.Wait()
		select {
		case done <- err:
		case <-ctx.Done():
			// Context cancelled, exit goroutine
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Info("process_terminated_with_error", "process", pm.cmd, "error", err)
		} else {
			slog.Info("process_gracefully_terminated", "process", pm.cmd)
		}
	case <-ctx.Done():
		// Force kill if graceful shutdown times out
		slog.Warn("force_killing_process_after_timeout", "process", pm.cmd, "pid", proc.Pid, "timeout", gracePeriod)
		pm.forceKillProcess(proc)

		// Give a brief moment for the force kill to take effect
		time.Sleep(100 * time.Millisecond)
		slog.Info("process_force_killed", "process", pm.cmd)
	}
}

// sendTerminationSignal sends a platform-appropriate termination signal
func (pm *ProcessManager) sendTerminationSignal(proc *os.Process) error {
	if runtime.GOOS == "windows" {
		// Windows: Check if process is still running before attempting termination
		if !pm.isProcessRunning(proc) {
			return nil // Process already terminated
		}

		// For PowerShell processes on Windows, force kill immediately
		// as they don't handle graceful termination well
		if strings.Contains(strings.ToLower(pm.cmd), "powershell") || strings.Contains(strings.ToLower(pm.cmd), "pwsh") {
			slog.Debug("powershell_force_kill_on_windows", "process", pm.cmd, "pid", proc.Pid)
			pm.forceKillProcess(proc)
			return fmt.Errorf("force killed powershell process")
		}

		// For other Windows processes, use timeout-based termination
		slog.Debug("windows_timeout_based_termination", "process", pm.cmd, "pid", proc.Pid)
		return nil
	} else {
		// Unix/Linux/macOS: Send SIGTERM for graceful shutdown
		err := proc.Signal(syscall.SIGTERM)
		if err != nil {
			slog.Warn("failed_to_send_sigterm", "process", pm.cmd, "pid", proc.Pid, "error", err)
		}
		return err
	}
}

// isProcessRunning checks if a process is still running (cross-platform)
func (pm *ProcessManager) isProcessRunning(proc *os.Process) bool {
	if runtime.GOOS == "windows" {
		// On Windows, we can check process state
		err := proc.Signal(syscall.Signal(0))
		return err == nil
	} else {
		// On Unix systems, signal 0 can be used to check if process exists
		err := proc.Signal(syscall.Signal(0))
		return err == nil
	}
}

// forceKillProcess forcefully terminates a process and its children with proper error handling
func (pm *ProcessManager) forceKillProcess(proc *os.Process) {
	if runtime.GOOS == "windows" {
		// Windows: Kill the process directly with retries for stubborn processes
		for i := 0; i < 3; i++ {
			if err := proc.Kill(); err != nil {
				if i == 2 { // Last attempt
					slog.Error("failed_to_kill_process_windows_final", "process", pm.cmd, "pid", proc.Pid, "error", err)
				} else {
					slog.Warn("failed_to_kill_process_windows_retry", "process", pm.cmd, "pid", proc.Pid, "attempt", i+1, "error", err)
					time.Sleep(50 * time.Millisecond)
					continue
				}
			} else {
				slog.Debug("process_killed_windows", "process", pm.cmd, "pid", proc.Pid, "attempt", i+1)
				break
			}
		}
	} else {
		// Unix/Linux/macOS: Try to kill process group, fallback to single process
		if pgid, err := getProcessGroupID(proc.Pid); err == nil {
			// Kill process group (negative PID)
			if err := killProcessGroup(pgid); err != nil {
				slog.Warn("failed_to_kill_process_group", "process", pm.cmd, "pgid", pgid, "error", err)
				// Fallback to single process kill
				if err := proc.Kill(); err != nil {
					slog.Error("failed_to_kill_process_fallback", "process", pm.cmd, "pid", proc.Pid, "error", err)
				}
			} else {
				slog.Debug("process_group_killed", "process", pm.cmd, "pgid", pgid)
			}
		} else {
			slog.Debug("no_process_group_found", "process", pm.cmd, "pid", proc.Pid, "error", err)
			// Fallback to killing just the process
			if err := proc.Kill(); err != nil {
				slog.Error("failed_to_kill_process_unix", "process", pm.cmd, "pid", proc.Pid, "error", err)
			}
		}
	}
}

// gracefulShutdown terminates the current process gracefully
func (pm *ProcessManager) gracefulShutdown() {
	pm.procMutex.RLock()
	proc := pm.currentProc
	pm.procMutex.RUnlock()

	if proc == nil {
		return
	}

	pm.terminateProcess(proc)
}

// Main function
// Loads commands from a file and starts a goroutine for each command
// Each goroutine starts the command and waits for it to finish
// If the command exits, it is restarted
// The program can be terminated by sending an OS signal (SIGTERM, SIGINT)
func main() {
	// Parse command line flags
	filePath := flag.String("f", "commands.txt", "file containing commands to run")
	gracePeriod := flag.Duration("grace", DefaultGracePeriod, "graceful shutdown timeout")
	restartDelay := flag.Duration("restart-delay", DefaultRestartInterval, "delay between process restarts")
	maxRetries := flag.Int("max-retries", 0, "maximum consecutive failures before giving up (0 = unlimited)")
	backoffEnabled := flag.Bool("backoff", true, "enable exponential backoff for failing processes")
	webDashboard := flag.Bool("dashboard", false, "enable web dashboard for monitoring")
	webPort := flag.Int("port", 8080, "web dashboard port")
	webHost := flag.String("host", "localhost", "web dashboard host")
	version := flag.Bool("version", false, "show version information")
	flag.Parse()

	// Handle version flag
	if *version {
		fmt.Printf("lars-script-runner version %s\n", Version)
		os.Exit(0)
	}

	// Validate configuration
	if err := validateConfig(*gracePeriod, *restartDelay, *maxRetries, *webPort, *webHost); err != nil {
		slog.Error("invalid_configuration", "error", err)
		os.Exit(1)
	}

	// Create configuration
	config := &Config{
		CommandFile:    *filePath,
		GracePeriod:    *gracePeriod,
		RestartDelay:   *restartDelay,
		MaxRetries:     *maxRetries,
		BackoffEnabled: *backoffEnabled,
		WebDashboard:   *webDashboard,
		WebPort:        *webPort,
		WebHost:        *webHost,
	}

	// Load commands from file
	commands, err := loadCommands(config.CommandFile)
	if err != nil {
		slog.Error("failed_to_load_commands", "error", err, "file", config.CommandFile)
		os.Exit(1)
	}

	if len(commands) == 0 {
		slog.Warn("no_commands_found", "file", config.CommandFile)
		fmt.Fprintf(os.Stderr, "No commands found in %s. Please add commands to monitor.\n", config.CommandFile)
		os.Exit(0)
	}

	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup

	// Create dashboard manager if enabled
	var dashboardMgr *DashboardManager
	if config.WebDashboard {
		dashboardMgr = NewDashboardManager(config)
		go func() {
			if err := dashboardMgr.Start(); err != nil && err != http.ErrServerClosed {
				slog.Error("dashboard_server_error", "error", err)
			}
		}()
		slog.Info("dashboard_started", "url", fmt.Sprintf("http://%s:%d", config.WebHost, config.WebPort))
	}

	// Start process managers with graceful shutdown support
	validProcesses := 0
	for i, cmd := range commands {
		processID := fmt.Sprintf("process_%d", i)
		pm, err := NewProcessManager(cmd, config, processID, dashboardMgr)
		if err != nil {
			slog.Error("failed_to_create_process_manager", "cmd", cmd, "error", err, "process_id", processID)
			continue
		}

		// Register with dashboard if enabled
		if dashboardMgr != nil {
			dashboardMgr.RegisterProcess(pm)
		}

		wg.Add(1)
		go pm.Start(ctx, &wg)
		validProcesses++
	}

	if validProcesses == 0 {
		slog.Error("no_valid_processes_to_start")
		fmt.Fprintf(os.Stderr, "No valid processes could be started. Please check your commands.\n")
		os.Exit(1)
	}

	slog.Info("processes_started", "total", len(commands), "valid", validProcesses)

	// Wait for shutdown signal
	switch <-sigCh {
	case os.Interrupt:
		slog.Info("signal_received", "signal", "SIGINT")
	case syscall.SIGTERM:
		slog.Info("signal_received", "signal", "SIGTERM")
	default:
		slog.Warn("signal_received", "signal", "UNKNOWN")
	}

	// Cancel context to trigger graceful shutdown
	slog.Info("initiating_graceful_shutdown")
	cancel()

	// Stop dashboard server if running
	if dashboardMgr != nil {
		if err := dashboardMgr.Stop(); err != nil {
			slog.Warn("dashboard_stop_error", "error", err)
		}
	}

	// Wait for all processes to shut down with timeout
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	shutdownTimeout := config.GracePeriod * 2 // Give extra time for all processes
	select {
	case <-done:
		slog.Info("all_processes_shutdown_gracefully")
	case <-time.After(shutdownTimeout):
		slog.Warn("shutdown_timeout_exceeded", "timeout", shutdownTimeout)
	}

	slog.Info("shutdown_complete")
	os.Exit(0)
}

// Load commands from a file
// Each line in the file is a command to run
// Empty lines and comments (starting with #) are ignored
// Returns error instead of calling os.Exit for better error handling
func loadCommands(filePath string) ([]string, error) {
	var commands []string

	// Print a message that we are loading commands from the file
	slog.Info("loading_commands", "file", filePath)

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		cmd := strings.TrimSpace(scanner.Text())

		// Ignore empty lines and lines starting with #
		if cmd == "" || strings.HasPrefix(cmd, "#") {
			continue
		}

		// Validate the command before adding it
		if err := validateCommand(cmd); err != nil {
			return nil, fmt.Errorf("invalid command on line %d: %w", lineNum, err)
		}

		commands = append(commands, cmd)
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan file %s: %w", filePath, err)
	}

	// Print a message that the commands have been loaded from the file
	slog.Info("commands_loaded", "file", filePath, "count", len(commands))

	return commands, nil
}

// validateCommand performs basic validation on commands to prevent injection attacks
// and ensure commands are properly formatted
func validateCommand(cmd string) error {
	// Check for empty command after trimming
	if strings.TrimSpace(cmd) == "" {
		return errors.New("command cannot be empty")
	}

	// Split command into parts
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return errors.New("command cannot be empty after parsing")
	}

	// Get the base command (first part)
	command := parts[0]

	// Check for dangerous characters that could indicate command injection
	dangerousChars := regexp.MustCompile(`[;&|><$\x60]`) // backticks, semicolons, pipes, redirects, etc.
	if dangerousChars.MatchString(cmd) {
		return fmt.Errorf("command contains potentially dangerous characters: %s", cmd)
	}

	// Check if command looks like a file path (basic validation)
	if strings.ContainsRune(command, filepath.Separator) || strings.Contains(command, "/") {
		// If it looks like a path, check if it's absolute or relative
		if !filepath.IsAbs(command) {
			// For relative paths, ensure they don't try to escape current directory
			if strings.Contains(command, "..") {
				return fmt.Errorf("relative paths with '..' are not allowed: %s", command)
			}
		}
	}

	// Check for excessively long commands (potential buffer overflow attempts)
	if len(cmd) > MaxCommandLength {
		return fmt.Errorf("command too long (%d characters, max %d): %s", len(cmd), MaxCommandLength, cmd[:50]+"...")
	}

	return nil
}

// validateConfig validates command line configuration parameters
func validateConfig(gracePeriod, restartDelay time.Duration, maxRetries, webPort int, webHost string) error {
	if gracePeriod < 0 {
		return fmt.Errorf("grace period cannot be negative: %v", gracePeriod)
	}
	if gracePeriod > 10*time.Minute {
		return fmt.Errorf("grace period too long (max 10 minutes): %v", gracePeriod)
	}

	if restartDelay < 0 {
		return fmt.Errorf("restart delay cannot be negative: %v", restartDelay)
	}
	if restartDelay > time.Hour {
		return fmt.Errorf("restart delay too long (max 1 hour): %v", restartDelay)
	}

	if maxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative: %d", maxRetries)
	}
	if maxRetries > 1000 {
		return fmt.Errorf("max retries too high (max 1000): %d", maxRetries)
	}

	if webPort < 1 || webPort > 65535 {
		return fmt.Errorf("web port must be between 1 and 65535: %d", webPort)
	}

	if strings.TrimSpace(webHost) == "" {
		return fmt.Errorf("web host cannot be empty")
	}

	return nil
}
