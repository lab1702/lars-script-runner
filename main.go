// Tiny program to run multiple commands in parallel and restart them if they exit.
// Created by Lars Bernhardsson during Christmas break, 2023.
// License: MIT

package main

import (
	"bufio"
	"flag"
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
	// Either use commands.txt or a user specified file
	filePath := flag.String("f", "commands.txt", "file containing commands to run")
	flag.Parse()

	// Create a wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Listen for OS signals to properly terminate goroutines on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Create a quit channel to stop goroutines
	quitCh := make(chan bool)

	// Start goroutines for each command
	for _, cmd := range loadCommands(*filePath) {
		wg.Add(1)
		go startProcess(cmd, &wg, quitCh)
	}

	// Wait for termination signals
	switch <-sigCh {
	case os.Interrupt:
		fmt.Println(time.Now(), "received os.Interrupt signal")
	case syscall.SIGINT:
		fmt.Println(time.Now(), "received syscall.SIGINT signal")
	case syscall.SIGTERM:
		fmt.Println(time.Now(), "received syscall.SIGTERM signal")
	default:
		fmt.Println(time.Now(), "received unknown signal")
	}

	// Tell all goroutines to exit
	fmt.Println(time.Now(), "telling goroutines to end")
	close(quitCh)

	// Wait for all goroutines to finish
	wg.Wait()

	fmt.Println(time.Now(), "all goroutines ended")

	// Exit the program
	os.Exit(0)
}

// Load commands from a file
// Each line in the file is a command to run
// Empty lines are ignored
func loadCommands(filePath string) []string {
	var commands []string

	fmt.Println(time.Now(), "opening file:", filePath)

	// Open the file
	file, err := os.Open(filePath)

	// If the file could not be opened, exit the program
	if err != nil {
		fmt.Println(time.Now(), "failed to open file:", filePath, "error:", err)
		os.Exit(1)
	}

	// Close the file when the function ends
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())

		// Ignore empty lines
		if cmd != "" {
			commands = append(commands, cmd)
		}
	}

	// If there was an error reading the file, exit the program
	if err := scanner.Err(); err != nil {
		fmt.Println(time.Now(), "failed to scan file:", filePath, "error:", err)
		os.Exit(1)
	}

	fmt.Println(time.Now(), "commands loaded from file:", filePath)

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
			fmt.Println(time.Now(), "exiting goroutine for process:", cmd)
			return
		default:
			// Print a message that we are starting the command
			fmt.Println(time.Now(), "starting process:", cmd)

			// Create command execution instance
			process := exec.Command(command, args...)
			process.Stdout = os.Stdout
			process.Stderr = os.Stderr

			// Start the process
			err := process.Start()

			// If the process could not be started, exit the goroutine
			if err != nil {
				fmt.Println(time.Now(), "failed to start process:", cmd, "error:", err)
				return
			}

			// Print a message that the process was started
			fmt.Println(time.Now(), "process started:", cmd)

			// Wait for the process to finish
			err = process.Wait()

			// If the process exited with or without an error, make a note of it before looping around to restart it
			if err != nil {
				fmt.Println(time.Now(), "process exited:", cmd, "error:", err)
			} else {
				fmt.Println(time.Now(), "process exited:", cmd)
			}
		}
	}
}
