package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestProcessManagerIntegration tests the full ProcessManager lifecycle
func TestProcessManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name        string
		command     string
		gracePeriod time.Duration
		testTimeout time.Duration
	}{
		{
			name:        "short running command",
			command:     "echo integration_test",
			gracePeriod: 1 * time.Second,
			testTimeout: 3 * time.Second,
		},
		{
			name:        "command with args",
			command:     "sleep 0.1",
			gracePeriod: 1 * time.Second,
			testTimeout: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				GracePeriod:    tt.gracePeriod,
				RestartDelay:   time.Second,
				MaxRetries:     0,
				BackoffEnabled: true,
			}
			pm, err := NewProcessManager(tt.command, config, "test-"+tt.name)
			if err != nil {
				t.Fatalf("Failed to create ProcessManager: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.testTimeout)
			defer cancel()

			var wg sync.WaitGroup
			wg.Add(1)

			// Start the process manager
			go pm.Start(ctx, &wg)

			// Let it run for a bit
			time.Sleep(500 * time.Millisecond)

			// Cancel context to trigger shutdown
			cancel()

			// Wait for shutdown with timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(tt.gracePeriod + 2*time.Second):
				t.Error("ProcessManager did not shut down within expected time")
			}
		})
	}
}

// TestGracefulShutdownWithRunningProcess tests graceful shutdown with a longer-running process
func TestGracefulShutdownWithRunningProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use a command that will run long enough to test graceful shutdown
	config := &Config{
		GracePeriod:    1 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	pm, err := NewProcessManager("sleep 5", config, "test-timeout")
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Start the process manager
	wg.Add(1)
	go pm.Start(ctx, &wg)

	// Wait for process to start
	time.Sleep(200 * time.Millisecond)

	// Verify process is running
	pm.procMutex.RLock()
	proc := pm.currentProc
	pm.procMutex.RUnlock()

	if proc == nil {
		t.Fatal("Expected process to be running")
	}

	// Trigger graceful shutdown
	cancel()

	// Wait for shutdown
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - process should have been terminated
	case <-time.After(5 * time.Second):
		t.Error("ProcessManager did not shut down within expected time")
	}

	// Verify process is no longer running
	pm.procMutex.RLock()
	finalProc := pm.currentProc
	pm.procMutex.RUnlock()

	if finalProc != nil {
		t.Error("Process should be nil after shutdown")
	}
}

// TestProcessRestartBehavior tests that processes restart after exiting
func TestProcessRestartBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use a command that exits quickly
	config := &Config{
		GracePeriod:    1 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	pm, err := NewProcessManager("echo restart_test", config, "test-restart")
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	// Start the process manager
	go pm.Start(ctx, &wg)

	// Let it run and restart a few times
	time.Sleep(2500 * time.Millisecond)

	// Cancel and wait for shutdown
	cancel()
	wg.Wait()

	// Test passes if no panics or deadlocks occurred
}

// TestPlatformSpecificBehavior tests platform-specific functionality
func TestPlatformSpecificBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := &Config{
		GracePeriod:    500 * time.Millisecond,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	pm, err := NewProcessManager("sleep 2", config, "test-shutdown")
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	// Create a test process
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, pm.command, pm.args...)

	// Test platform-specific attributes
	pm.setPlatformProcessAttrs(cmd)

	// Verify attributes were set correctly based on platform
	if runtime.GOOS == "windows" {
		// On Windows, SysProcAttr might be nil or empty
		if cmd.SysProcAttr != nil {
			t.Logf("Windows: SysProcAttr set to %+v", cmd.SysProcAttr)
		}
	} else {
		// On Unix-like systems, SysProcAttr should be set with Setpgid
		if cmd.SysProcAttr == nil {
			t.Error("Expected SysProcAttr to be set on Unix-like systems")
		} else if !cmd.SysProcAttr.Setpgid {
			t.Error("Expected Setpgid to be true on Unix-like systems")
		}
	}
}

// TestSignalHandlingIntegration tests signal handling behavior
func TestSignalHandlingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if runtime.GOOS == "windows" {
		t.Skip("Signal testing not applicable on Windows")
	}

	config := &Config{
		GracePeriod:    1 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	pm, err := NewProcessManager("sleep 10", config, "test-context")
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	// Start a process
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, pm.command, pm.args...)
	pm.setPlatformProcessAttrs(cmd)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}

	// Test sending termination signal
	err = pm.sendTerminationSignal(cmd.Process)
	if err != nil {
		t.Errorf("Failed to send termination signal: %v", err)
	}

	// Wait a bit and then force kill
	time.Sleep(200 * time.Millisecond)
	pm.forceKillProcess(cmd.Process)

	// Wait for process to finish
	cmd.Wait()
}

// TestConcurrentProcessManagers tests multiple ProcessManagers running concurrently
func TestConcurrentProcessManagers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const numManagers = 3
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	commands := []string{
		"echo concurrent_test_1",
		"echo concurrent_test_2",
		"echo concurrent_test_3",
	}

	for i, cmd := range commands {
		config := &Config{
			GracePeriod:    500 * time.Millisecond,
			RestartDelay:   time.Second,
			MaxRetries:     0,
			BackoffEnabled: true,
		}
		pm, err := NewProcessManager(cmd, config, fmt.Sprintf("concurrent-%d", i))
		if err != nil {
			t.Fatalf("Failed to create ProcessManager %d: %v", i, err)
		}

		wg.Add(1)
		go pm.Start(ctx, &wg)
	}

	// Let them run for a bit
	time.Sleep(1 * time.Second)

	// Cancel and wait for all to finish
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Error("Not all ProcessManagers shut down within expected time")
	}
}

// TestLoadCommandsIntegration tests loading commands from actual files
func TestLoadCommandsIntegration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "lars-integration-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with a realistic commands file
	testFile := filepath.Join(tmpDir, "integration_commands.txt")
	content := `# Integration test commands
echo "Starting integration test"
sleep 0.1
echo "Integration test complete"`

	err = os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	commands, err := loadCommands(testFile)
	if err != nil {
		t.Fatalf("Failed to load commands: %v", err)
	}

	expectedCommands := []string{
		"echo \"Starting integration test\"",
		"sleep 0.1",
		"echo \"Integration test complete\"",
	}

	if len(commands) != len(expectedCommands) {
		t.Errorf("Expected %d commands, got %d", len(expectedCommands), len(commands))
	}

	for i, expected := range expectedCommands {
		if i >= len(commands) || commands[i] != expected {
			t.Errorf("Command %d: expected %q, got %q", i, expected, commands[i])
		}
	}

	// Test that we can create ProcessManagers from loaded commands
	config := &Config{
		GracePeriod:    1 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	for i, cmd := range commands {
		pm, err := NewProcessManager(cmd, config, fmt.Sprintf("bench-%d", i))
		if err != nil {
			t.Errorf("Failed to create ProcessManager for command %d (%q): %v", i, cmd, err)
		}
		if pm == nil {
			t.Errorf("ProcessManager %d should not be nil", i)
		}
	}
}

// BenchmarkProcessManagerStart benchmarks the ProcessManager.Start method
func BenchmarkProcessManagerStart(b *testing.B) {
	config := &Config{
		GracePeriod:    1 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}
	pm, err := NewProcessManager("echo benchmark", config, "benchmark")
	if err != nil {
		b.Fatalf("Failed to create ProcessManager: %v", err)
	}

	b.ResetTimer()

	for range b.N {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		var wg sync.WaitGroup
		wg.Add(1)

		go pm.Start(ctx, &wg)

		// Let it run briefly then cancel
		time.Sleep(50 * time.Millisecond)
		cancel()
		wg.Wait()
	}
}
