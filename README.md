# lars-script-runner

Tiny command line tool that will make sure a list of scripts or other commands are always running.

If one of the commands exits, with or without errors, this tool will make sure each command is restarted no more often than once per second.

The functionality can most likely be replicated with a small shell or powershell script,
but I wanted to learn a bit more about [Go](https://go.dev/) and create a cross-platform solution that works reliably across different operating systems.

## Installation

    go install github.com/lab1702/lars-script-runner@latest

## Downloading source and running the example:

    git clone https://github.com/lab1702/lars-script-runner.git
    cd lars-script-runner
    go build
    ./lars-script-runner

If all works, to install do:

    go install

## How to configure what commands to keep alive:

Edit the **[commands.txt](commands.txt)** file to contain all the commands you want to have running at all times, putting one command on each line.

## Command Line Options:

    ./lars-script-runner [options]

Available options:
- `-f string` - file containing commands to run (default "commands.txt")
- `-dashboard` - enable web dashboard for monitoring
- `-port int` - web dashboard port (default 8080)
- `-host string` - web dashboard host (default "localhost")
- `-grace duration` - graceful shutdown timeout (default 5s)
- `-restart-delay duration` - delay between process restarts (default 1s)
- `-max-retries int` - maximum consecutive failures before giving up (0 = unlimited)
- `-backoff` - enable exponential backoff for failing processes (default true)
- `-version` - show version information

## Web Dashboard:

Enable the optional web dashboard to monitor and manage processes:

    ./lars-script-runner -dashboard -port 8080

Features:
- Real-time process monitoring with status, uptime, and failure counts
- Restart individual processes from the web interface
- Responsive design that works on desktop and mobile
- Live updates every 3 seconds via HTTP polling

## Compatibility:

This tool runs on **Linux**, **Windows**, and **macOS** on both **AMD64** and **ARM64** architectures.

Developed and tested on:
- Windows Server 2022
- Ubuntu 22.04 LTS  
- macOS (Intel and Apple Silicon)

The **[commands.txt](commands.txt)** file can contain any executable commands - bash scripts, PowerShell scripts, binaries, etc.
