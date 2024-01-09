// Tiny program to run multiple commands in parallel and restart them if they exit.

package main

import (
	"bufio"
	"fmt"
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
	// File containing commands to run
	filePath := "commands.txt"

	// Create a wait group to wait for all processes to finish
	var wg sync.WaitGroup

	// Listen for OS signals to properly terminate processes on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Create a quit channel to stop goroutines
	quitCh := make(chan bool)

	// Start goroutines for each command
	for _, cmd := range loadCommands(filePath) {
		wg.Add(1)
		go startProcess(cmd, &wg, quitCh)
	}

	// Wait for termination signals
	<-sigCh

	// Stop all goroutines
	fmt.Println(time.Now(), "Received termination signal. Telling goroutines to end...")
	close(quitCh)

	wg.Wait() // Wait for ongoing processes to finish before exiting

	fmt.Println(time.Now(), "All goroutines ended.")

	os.Exit(0)
}

// Load commands from a file
// Each line in the file is a command to run
// Empty lines are ignored
func loadCommands(filePath string) []string {
	var commands []string

	file, err := os.Open(filePath)

	if err != nil {
		fmt.Println(time.Now(), "Error opening file:", err)
		os.Exit(1)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())

		if cmd != "" {
			commands = append(commands, cmd)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println(time.Now(), "Error scanning file:", err)
		os.Exit(1)
	}

	fmt.Println(time.Now(), "List of commands loaded.")

	return commands
}

func startProcess(cmd string, wg *sync.WaitGroup, quit <-chan bool) {
	defer wg.Done()

	for {
		select {
		case <-quit:
			return
		default:
			// Split the command string into command and arguments
			parts := strings.Fields(cmd)
			command := parts[0]
			args := parts[1:]

			// Create command execution instance
			process := exec.Command(command, args...)
			process.Stdout = os.Stdout
			process.Stderr = os.Stderr

			// Start the process
			err := process.Start()

			if err != nil {
				fmt.Println(time.Now(), "Error starting process: ", err)
				return
			} else {
				fmt.Printf("%s Process %s started.\n", time.Now(), cmd)
			}

			// Wait for the process to finish
			err = process.Wait()

			if err != nil {
				fmt.Printf("%s Process %s exited with error: %s. Restarting...\n", time.Now(), cmd, err)
			} else {
				fmt.Printf("%s Process %s exited successfully. Restarting...\n", time.Now(), cmd)
			}
		}
	}
}
