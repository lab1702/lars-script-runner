// Tiny program to run multiple commands in parallel and restart them if they exit.
// Created by Lars Bernhardsson during Christmas break, 2023.
// License: MIT

package main

import (
	"bufio"
	"flag"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Main function
// Loads commands from a file and starts a goroutine for each command
// Each goroutine starts the command and waits for it to finish
// If the command exits, it is restarted
// The program can be terminated by sending an OS signal (SIGTERM, SIGINT)
func main() {
	// Either use commands.txt or a user specified file
	filePath := flag.String("f", "commands.txt", "file containing commands to run")
	flag.Parse()

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Create a channel to listen for termination signals
	sigCh := make(chan os.Signal, 1)

	// Listen for SIGINT and SIGTERM
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Create a channel to tell all goroutines to exit
	quitCh := make(chan bool)

	// Start goroutines for each command
	for _, cmd := range loadCommands(*filePath) {
		// Add a goroutine to the wait group
		wg.Add(1)

		// Start the goroutine
		go startProcess(cmd, &wg, quitCh)
	}

	// Wait for termination signals
	switch <-sigCh {
	case os.Interrupt:
		slog.Info("signal_received", "signal", "os.Interrupt")
	case syscall.SIGINT:
		slog.Info("signal_received", "signal", "syscall.SIGINT")
	case syscall.SIGTERM:
		slog.Info("signal_received", "signal", "syscall.SIGTERM")
	default:
		slog.Warn("signal_received", "signal", "UNKNOWN")
	}

	// Tell all goroutines to exit
	slog.Info("closing_quit_channel")
	close(quitCh)

	// Print a message that we are waiting for all goroutines to finish
	slog.Info("waiting_goroutines_exit")

	// Wait for all goroutines to finish
	wg.Wait()

	// Print a message that all goroutines have finished
	slog.Info("all_goroutines_exited")

	// Exit the program
	os.Exit(0)
}

// Load commands from a file
// Each line in the file is a command to run
// Empty lines are ignored
func loadCommands(filePath string) []string {
	var commands []string

	// Print a message that we are loading commands from the file
	slog.Info("loading_commands", "file", filePath)

	// Open the file
	file, err := os.Open(filePath)

	// If the file could not be opened, exit the program
	if err != nil {
		slog.Error("failed_to_open", "file", filePath, "error", err)
		os.Exit(1)
	}

	// Close the file when the function ends
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)

	// For each line, add the command to the list of commands
	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())

		// Ignore empty lines and lines starting with #
		if cmd != "" && !strings.HasPrefix(cmd, "#") {
			commands = append(commands, cmd)
		}
	}

	// If there was an error reading the file, exit the program
	if err := scanner.Err(); err != nil {
		slog.Error("failed_to_scan", "file", filePath, "error", err)
		os.Exit(1)
	}

	// Print a message that the commands have been loaded from the file
	slog.Info("commands_loaded", "file", filePath)

	// Return the list of commands
	return commands
}

func startProcess(cmd string, wg *sync.WaitGroup, quit <-chan bool) {
	// Tell the wait group that this goroutine is done when the function ends
	defer wg.Done()

	// Split the command string into command and arguments
	parts := strings.Fields(cmd)
	command := parts[0]
	args := parts[1:]

	// Create a ticker to only allow one restart attempt per second
	ticker := time.NewTicker(time.Second)

	// Close the ticker when the function ends
	defer ticker.Stop()

	// Endless for loop to restart the command if it exits
	// The loop can be exited by sending a value to the quit channel
	// or if there are any errors starting the command
	for {
		// make sure we don't try to restart the command more than once per second
		<-ticker.C

		// Check if the goroutine is being told to exit.
		select {
		case <-quit:
			slog.Info("exiting_goroutine", "process", cmd)
			return
		default:
			// Print a message that we are starting the command
			slog.Info("starting_process", "process", cmd)

			// Create command execution instance
			process := exec.Command(command, args...)

			// Set the standard output and error to the same as the parent process
			process.Stdout = os.Stdout
			process.Stderr = os.Stderr

			// Start the process
			err := process.Start()

			// If the process could not be started, exit the goroutine
			if err != nil {
				slog.Warn("process_failed", "process", cmd, "error", err)
				return
			}

			// Print a message that the process was started
			slog.Info("process_started", "process", cmd)

			// Wait for the process to finish
			err = process.Wait()

			// If the process exited with or without an error, make a note of it before looping around to restart it
			if err != nil {
				slog.Warn("process_exited_error", "process", cmd, "error", err)
			} else {
				slog.Warn("process_exited_normal", "process", cmd)
			}
		}
	}
}
