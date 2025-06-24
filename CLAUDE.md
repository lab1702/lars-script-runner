# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is `lars-script-runner`, a simple Go command-line tool that monitors and keeps a list of commands running continuously. When any command exits (with or without errors), the tool automatically restarts it, but no more than once per second to prevent rapid restart loops.

## Build and Run Commands

- **Build**: `go build`
- **Run with default config**: `./lars-script-runner`
- **Run with custom command file**: `./lars-script-runner -f /path/to/commands.txt`
- **Run with web dashboard**: `./lars-script-runner -dashboard -port 8080`
- **Show version**: `./lars-script-runner -version`
- **Install locally**: `go install`
- **Install from remote**: `go install github.com/lab1702/lars-script-runner@v2.0.0`

## Command Line Options

The application supports the following command-line flags:

- `-f string` - file containing commands to run (default "commands.txt")
- `-dashboard` - enable web dashboard for monitoring
- `-port int` - web dashboard port (default 8080)
- `-host string` - web dashboard host (default "localhost")
- `-grace duration` - graceful shutdown timeout (default 5s)
- `-restart-delay duration` - delay between process restarts (default 1s)
- `-max-retries int` - maximum consecutive failures before giving up (0 = unlimited)
- `-backoff` - enable exponential backoff for failing processes (default true)
- `-version` - show version information

## Architecture

The application consists of these main files:

- `main.go`: Core process management, configuration, and command handling
- `dashboard.go`: Web dashboard for monitoring and controlling processes
- `platform_unix.go`: Unix/Linux/macOS specific syscalls and process management
- `platform_windows.go`: Windows specific syscalls and process management

### Key Design Patterns

1. **Goroutine per Command**: Each command runs in its own goroutine for parallel execution
2. **Signal Handling**: Gracefully handles SIGINT and SIGTERM for clean shutdown
3. **Rate Limiting**: Uses `time.Ticker` to prevent commands from restarting more than once per second
4. **Structured Logging**: Uses `log/slog` for consistent structured logging throughout
5. **Security**: Input validation, path traversal protection, and security headers

### Configuration

Commands are specified in `commands.txt` (or custom file via `-f` flag):
- One command per line
- Empty lines are ignored
- Lines starting with `#` are treated as comments and ignored
- Commands are parsed using `strings.Fields()` to separate command from arguments

### Cross-Platform Compatibility

The tool is designed to work on Windows, macOS, and Linux on both AMD64 and ARM64 architectures. Platform-specific code is isolated in separate files using Go build tags:

- **Unix/Linux/macOS**: Uses `platform_unix.go` with build tag `//go:build !windows`
- **Windows**: Uses `platform_windows.go` with build tag `//go:build windows`

This approach ensures proper syscall handling across platforms while maintaining a clean separation of concerns. The example PowerShell scripts (test1.ps1, test2.ps1, test3.ps1, test4.ps1) demonstrate cross-platform usage when PowerShell is available.

## Web Dashboard

Lars Script Runner includes an optional web dashboard for monitoring and managing processes:

### Features

- **Real-time Process Monitoring**: View status, uptime, restart count, and failure count
- **Process Management**: Restart individual processes from the web interface
- **Live Updates**: HTTP polling-based real-time updates every 3 seconds
- **Process Statistics**: Detailed metrics including PID, start time, and last failure
- **Responsive Design**: Works on desktop and mobile devices
- **Local Timezone Display**: All timestamps shown in local timezone
- **Vibrant Status Colors**: Easy-to-distinguish process status indicators

### Usage

- **Enable Dashboard**: Use the `-dashboard` flag
- **Set Port**: Use `-port 8080` (default: 8080)
- **Set Host**: Use `-host localhost` (default: localhost)
- **Full Example**: `./lars-script-runner -dashboard -port 9000 -host 0.0.0.0`

### API Endpoints

- `GET /` - Main dashboard interface
- `GET /api/processes` - JSON list of all processes
- `GET /api/process/{id}` - JSON details for specific process
- `POST /api/restart/{id}` - Restart specific process
- `GET /static/style.css` - Dashboard CSS styles
- `GET /static/script.js` - Dashboard JavaScript

### Dashboard Architecture

The web dashboard consists of:

1. **DashboardManager**: Manages HTTP server and client connections
2. **ProcessStats**: Tracks process metrics and status with local timezone display
3. **HTTP Polling**: Reliable 3-second polling for real-time updates
4. **REST API**: JSON endpoints for process information and control
5. **Responsive Frontend**: HTML/CSS/JavaScript dashboard interface with vibrant status colors
6. **Security Features**: Input validation, path traversal protection, and security headers

### Security Features

- Process ID validation to prevent path traversal attacks
- Security headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection)
- Input sanitization for static file serving
- Command validation to prevent injection attacks
- Structured error responses