package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
)

var commands []string

func main() {
	// Load commands from a file (one command per line)
	filePath := "commands.txt" // Update this with your file path
	loadCommands(filePath)

	// Create a wait group to wait for all processes to finish
	var wg sync.WaitGroup

	// Listen for OS signals to properly terminate processes on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, os.Kill)

	// Start processes for each command
	for _, cmd := range commands {
		wg.Add(1)
		go startProcess(cmd, &wg)
	}

	// Wait for termination signals or processes to finish
	select {
	case <-sigCh:
		// Terminate all processes on receiving an interrupt or termination signal
		fmt.Println("Received termination signal. Killing processes...")
		wg.Wait() // Wait for ongoing processes to finish before exiting
		fmt.Println("All processes terminated.")
	}
}

func loadCommands(filePath string) {
	file, err := os.Open(filePath)

	if err != nil {
		fmt.Println("Error opening file:", err)
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
		fmt.Println("Error scanning file:", err)
		os.Exit(1)
	}
}

func startProcess(cmd string, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
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
			fmt.Println("Error starting process: ", err)
			return
		}

		// Wait for the process to finish
		err = process.Wait()

		if err != nil {
			fmt.Printf("Process %s exited with error: %s. Restarting...\n", cmd, err)
		} else {
			fmt.Printf("Process %s exited successfully. Restarting...\n", cmd)
		}
	}
}
