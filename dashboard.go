package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// DashboardManager manages the web dashboard and process monitoring
type DashboardManager struct {
	processes    map[string]*ProcessManager
	mutex        sync.RWMutex
	server       *http.Server
	config       *Config
	stopUpdates  chan bool
}

// NewDashboardManager creates a new dashboard manager
func NewDashboardManager(config *Config) *DashboardManager {
	return &DashboardManager{
		processes:   make(map[string]*ProcessManager),
		stopUpdates: make(chan bool, 1),
		config:      config,
	}
}

// RegisterProcess adds a process to the dashboard monitoring
func (dm *DashboardManager) RegisterProcess(pm *ProcessManager) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	dm.processes[pm.id] = pm
}

// UnregisterProcess removes a process from dashboard monitoring
func (dm *DashboardManager) UnregisterProcess(id string) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	delete(dm.processes, id)
}

// Start starts the web dashboard server
func (dm *DashboardManager) Start() error {
	mux := http.NewServeMux()

	// Serve dashboard HTML
	mux.HandleFunc("/", dm.handleDashboard)

	// API endpoints
	mux.HandleFunc("/api/processes", dm.handleProcesses)
	mux.HandleFunc("/api/process/", dm.handleProcess)
	mux.HandleFunc("/api/restart/", dm.handleRestart)

	// Static files
	mux.HandleFunc("/static/", dm.handleStatic)

	dm.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", dm.config.WebHost, dm.config.WebPort),
		Handler: mux,
	}

	// Start periodic update broadcast for live uptime updates
	go dm.startPeriodicUpdates()

	return dm.server.ListenAndServe()
}

// Stop stops the web dashboard server with proper cleanup
func (dm *DashboardManager) Stop() error {
	if dm.server == nil {
		return nil
	}

	// Stop periodic updates
	select {
	case dm.stopUpdates <- true:
	default:
	}

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt graceful shutdown first
	if err := dm.server.Shutdown(ctx); err != nil {
		// Force close if graceful shutdown fails
		return dm.server.Close()
	}

	return nil
}

// handleDashboard serves the main dashboard page
func (dm *DashboardManager) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Security headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	tmpl, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		http.Error(w, "Template parsing error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Title   string
		WebPort int
	}{
		Title:   "Lars Script Runner Dashboard",
		WebPort: dm.config.WebPort,
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
	}
}

// handleProcesses returns JSON list of all processes
func (dm *DashboardManager) handleProcesses(w http.ResponseWriter, r *http.Request) {
	type ProcessInfo struct {
		ID      string       `json:"id"`
		Command string       `json:"command"`
		Stats   ProcessStats `json:"stats"`
	}

	// Collect process data with minimal lock time
	dm.mutex.RLock()
	processCount := len(dm.processes)
	processMap := make(map[string]*ProcessManager, processCount)
	for id, pm := range dm.processes {
		processMap[id] = pm
	}
	dm.mutex.RUnlock()

	// Build response without holding the main mutex
	processes := make([]ProcessInfo, 0, len(processMap))
	for id, pm := range processMap {
		// Use GetStats method to safely get stats with uptime calculation
		stats := pm.GetStats()

		processes = append(processes, ProcessInfo{
			ID:      id,
			Command: pm.cmd,
			Stats:   stats,
		})
	}

	// Sort processes alphabetically by command
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].Command < processes[j].Command
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	if err := json.NewEncoder(w).Encode(processes); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
	}
}

// handleProcess returns details for a specific process
func (dm *DashboardManager) handleProcess(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/process/"):]
	
	// Validate process ID format (security: prevent path traversal)
	if len(id) == 0 || len(id) > 50 || strings.ContainsAny(id, "../\\?*|<>:") {
		http.Error(w, "Invalid process ID", http.StatusBadRequest)
		return
	}

	dm.mutex.RLock()
	pm, exists := dm.processes[id]
	dm.mutex.RUnlock()

	if !exists {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	// Use GetStats method for safe access
	stats := pm.GetStats()

	response := struct {
		ID      string       `json:"id"`
		Command string       `json:"command"`
		Stats   ProcessStats `json:"stats"`
	}{
		ID:      id,
		Command: pm.cmd,
		Stats:   stats,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
	}
}

// handleRestart restarts a specific process
func (dm *DashboardManager) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/api/restart/"):]
	
	// Validate process ID format (security: prevent path traversal)
	if len(id) == 0 || len(id) > 50 || strings.ContainsAny(id, "../\\?*|<>:") {
		http.Error(w, "Invalid process ID", http.StatusBadRequest)
		return
	}

	dm.mutex.RLock()
	pm, exists := dm.processes[id]
	dm.mutex.RUnlock()

	if !exists {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	// Force restart by killing current process
	pm.procMutex.RLock()
	proc := pm.currentProc
	pm.procMutex.RUnlock()

	if proc != nil {
		pm.terminateProcess(proc)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "restarted"}); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
	}
}

// handleWebSocket handles WebSocket connections for real-time updates
func (dm *DashboardManager) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Disable WebSocket for now - use HTTP polling instead
	http.Error(w, "WebSocket temporarily disabled - use HTTP polling", http.StatusServiceUnavailable)
}



// BroadcastUpdate sends updates to all connected WebSocket clients (disabled)
func (dm *DashboardManager) BroadcastUpdate() {
	// WebSocket functionality disabled - clients will use HTTP polling
}

// handleStatic serves static files (CSS, JS, etc.)
func (dm *DashboardManager) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/static/"):]
	
	// Security: validate path to prevent directory traversal
	if strings.Contains(path, "..") || strings.Contains(path, "/") || strings.Contains(path, "\\") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "public, max-age=3600") // Cache static files for 1 hour

	switch path {
	case "style.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write([]byte(dashboardCSS))
	case "script.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write([]byte(dashboardJS))
	default:
		http.NotFound(w, r)
	}
}

// startPeriodicUpdates sends periodic updates to connected clients for live uptime updates (disabled)
func (dm *DashboardManager) startPeriodicUpdates() {
	// WebSocket periodic updates disabled - using HTTP polling instead
	<-dm.stopUpdates // Just wait for stop signal
}

// Dashboard HTML template
const dashboardHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div class="container">
        <header>
            <h1>{{.Title}}</h1>
            <div class="status-indicator">
                <span id="connection-status" class="status connected">Connected</span>
            </div>
        </header>
        
        <main>
            <div class="processes-grid" id="processes-grid">
                <div class="loading">Loading processes...</div>
            </div>
        </main>
    </div>
    
    <script>
        const WS_PORT = {{.WebPort}};
    </script>
    <script src="/static/script.js"></script>
</body>
</html>
`

// Dashboard CSS
const dashboardCSS = `
* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

:root {
    --bg-color: #f5f5f5;
    --text-color: #333;
    --card-bg: white;
    --header-color: #2c3e50;
    --border-color: rgba(0,0,0,0.1);
    --code-bg: #f8f9fa;
    --shadow: 0 2px 4px rgba(0,0,0,0.1);
    --shadow-hover: 0 4px 8px rgba(0,0,0,0.15);
}

@media (prefers-color-scheme: dark) {
    :root {
        --bg-color: #1a1a1a;
        --text-color: #e0e0e0;
        --card-bg: #2d2d2d;
        --header-color: #60a5fa;
        --border-color: rgba(255,255,255,0.1);
        --code-bg: #3a3a3a;
        --shadow: 0 2px 4px rgba(0,0,0,0.3);
        --shadow-hover: 0 4px 8px rgba(0,0,0,0.4);
    }
}

body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: var(--bg-color);
    color: var(--text-color);
    line-height: 1.6;
}

.container {
    max-width: 1200px;
    margin: 0 auto;
    padding: 20px;
}

header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 30px;
    padding: 20px;
    background: var(--card-bg);
    border-radius: 8px;
    box-shadow: var(--shadow);
}

h1 {
    color: var(--header-color);
    font-size: 2em;
    font-weight: 300;
}

.status-indicator {
    display: flex;
    align-items: center;
    gap: 10px;
}

.status {
    padding: 6px 12px;
    border-radius: 20px;
    font-size: 0.9em;
    font-weight: 500;
}

.status.connected {
    background: #28a745;
    color: white;
    font-weight: 600;
}

.status.disconnected {
    background: #dc3545;
    color: white;
    font-weight: 600;
}

.processes-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
    gap: 20px;
}

.process-card {
    background: var(--card-bg);
    border-radius: 8px;
    box-shadow: var(--shadow);
    padding: 20px;
    transition: transform 0.2s ease;
}

.process-card:hover {
    transform: translateY(-2px);
    box-shadow: var(--shadow-hover);
}

.process-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 15px;
}

.process-command {
    font-family: 'Monaco', 'Menlo', monospace;
    font-size: 0.9em;
    background: var(--code-bg);
    padding: 8px 12px;
    border-radius: 4px;
    word-break: break-all;
    flex: 1;
    margin-right: 10px;
}

.process-status {
    padding: 4px 8px;
    border-radius: 12px;
    font-size: 0.8em;
    font-weight: 500;
    text-transform: uppercase;
    white-space: nowrap;
}

.status-running {
    background: #28a745;
    color: white;
    font-weight: 600;
}

.status-stopped {
    background: #6c757d;
    color: white;
    font-weight: 600;
}

.status-failed {
    background: #dc3545;
    color: white;
    font-weight: 600;
}

.status-starting {
    background: #ffc107;
    color: #212529;
    font-weight: 600;
}

.process-stats {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 15px;
    margin-bottom: 15px;
}

.stat-item {
    text-align: center;
}

.stat-value {
    font-size: 1.5em;
    font-weight: 600;
    color: var(--header-color);
}

.stat-label {
    font-size: 0.8em;
    color: var(--text-color);
    opacity: 0.7;
    text-transform: uppercase;
    letter-spacing: 0.5px;
}

.process-actions {
    display: flex;
    gap: 10px;
    margin-top: 15px;
}

.btn {
    padding: 8px 16px;
    border: none;
    border-radius: 4px;
    font-size: 0.9em;
    cursor: pointer;
    transition: background-color 0.2s ease;
    flex: 1;
}

.btn-restart {
    background: #007bff;
    color: white;
}

.btn-restart:hover {
    background: #0056b3;
}

.btn-restart:disabled {
    background: #6c757d;
    cursor: not-allowed;
}

.loading {
    text-align: center;
    padding: 40px;
    color: var(--text-color);
    opacity: 0.7;
    font-size: 1.1em;
}

.uptime {
    font-size: 0.9em;
    color: var(--text-color);
    opacity: 0.8;
    margin-top: 10px;
}

@media (max-width: 768px) {
    .container {
        padding: 10px;
    }
    
    header {
        flex-direction: column;
        gap: 15px;
        text-align: center;
    }
    
    .processes-grid {
        grid-template-columns: 1fr;
    }
    
    .process-stats {
        grid-template-columns: 1fr;
    }
}
`

// Dashboard JavaScript
const dashboardJS = `
class Dashboard {
    constructor() {
        this.refreshInterval = 3000; // 3 seconds
        this.refreshTimer = null;
        this.init();
    }
    
    init() {
        console.log('Dashboard initializing with HTTP polling...');
        this.loadProcesses();
        this.startPolling();
    }
    
    startPolling() {
        if (this.refreshTimer) {
            clearInterval(this.refreshTimer);
        }
        
        this.refreshTimer = setInterval(() => {
            this.loadProcesses();
        }, this.refreshInterval);
        
        console.log('Started polling every', this.refreshInterval / 1000, 'seconds');
    }
    
    updateConnectionStatus(connected) {
        const status = document.getElementById('connection-status');
        if (status) {
            if (connected) {
                status.textContent = 'Connected (HTTP)';
                status.className = 'status connected';
            } else {
                status.textContent = 'Connecting...';
                status.className = 'status disconnected';
            }
        }
    }
    
    stopPolling() {
        if (this.refreshTimer) {
            clearInterval(this.refreshTimer);
            this.refreshTimer = null;
        }
    }
    
    
    async loadProcesses() {
        try {
            const response = await fetch('/api/processes', {
                method: 'GET',
                headers: {
                    'Cache-Control': 'no-cache',
                    'Pragma': 'no-cache'
                }
            });
            if (!response.ok) {
                throw new Error('HTTP ' + response.status + ': ' + response.statusText);
            }
            const processes = await response.json();
            this.updateProcesses(processes);
            this.updateConnectionStatus(true);
        } catch (error) {
            console.error('Failed to load processes:', error);
            this.updateConnectionStatus(false);
            const grid = document.getElementById('processes-grid');
            if (grid && grid.innerHTML.includes('Loading')) {
                grid.innerHTML = '<div class="loading">Failed to load processes. Retrying...</div>';
            }
        }
    }
    
    updateProcesses(processes) {
        const grid = document.getElementById('processes-grid');
        
        if (processes.length === 0) {
            grid.innerHTML = '<div class="loading">No processes configured</div>';
            return;
        }
        
        grid.innerHTML = processes.map(process => this.renderProcessCard(process)).join('');
        
        // Add event listeners for restart buttons
        processes.forEach(process => {
            const restartBtn = document.getElementById('restart-' + process.id);
            if (restartBtn) {
                restartBtn.addEventListener('click', () => this.restartProcess(process.id));
            }
        });
    }
    
    formatTimestamp(timestamp) {
        if (!timestamp || timestamp === null || timestamp === undefined) return 'Never';
        
        const date = new Date(timestamp);
        
        // Check for invalid date or zero time (Go's zero time is 0001-01-01T00:00:00Z)
        if (isNaN(date.getTime()) || date.getFullYear() === 1) return 'Never';
        
        return date.toLocaleString(undefined, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            timeZoneName: 'short'
        });
    }
    
    renderProcessCard(process) {
        const uptime = this.formatUptime(process.stats.uptime);
        const lastFailure = this.formatTimestamp(process.stats.last_failure);
        const startTime = this.formatTimestamp(process.stats.start_time);
        
        return ` + "`" + `
            <div class="process-card">
                <div class="process-header">
                    <div class="process-command">${this.escapeHtml(process.command)}</div>
                    <div class="process-status status-${process.stats.status}">${process.stats.status}</div>
                </div>
                
                <div class="process-stats">
                    <div class="stat-item">
                        <div class="stat-value">${process.stats.restart_count}</div>
                        <div class="stat-label">Restarts</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${process.stats.failure_count}</div>
                        <div class="stat-label">Failures</div>
                    </div>
                </div>
                
                <div class="uptime">
                    <strong>Uptime:</strong> ${uptime}<br>
                    <strong>PID:</strong> ${process.stats.pid || 'N/A'}<br>
                    <strong>Started:</strong> ${startTime}<br>
                    <strong>Last Failure:</strong> ${lastFailure}
                </div>
                
                <div class="process-actions">
                    <button id="restart-${process.id}" class="btn btn-restart">
                        Restart Process
                    </button>
                </div>
            </div>
        ` + "`" + `;
    }
    
    async restartProcess(processId) {
        const restartBtn = document.getElementById('restart-' + processId);
        if (restartBtn) {
            restartBtn.disabled = true;
            restartBtn.textContent = 'Restarting...';
        }
        
        try {
            const response = await fetch('/api/restart/' + processId, {
                method: 'POST'
            });
            
            if (response.ok) {
                console.log('Process restarted successfully');
                // Refresh after a short delay
                setTimeout(() => this.loadProcesses(), 1000);
            } else {
                console.error('Failed to restart process');
            }
        } catch (error) {
            console.error('Error restarting process:', error);
        } finally {
            if (restartBtn) {
                restartBtn.disabled = false;
                restartBtn.textContent = 'Restart Process';
            }
        }
    }
    
    formatUptime(nanoseconds) {
        const seconds = Math.floor(nanoseconds / 1000000000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);
        
        if (days > 0) {
            return days + 'd ' + (hours % 24) + 'h ' + (minutes % 60) + 'm';
        } else if (hours > 0) {
            return hours + 'h ' + (minutes % 60) + 'm ' + (seconds % 60) + 's';
        } else if (minutes > 0) {
            return minutes + 'm ' + (seconds % 60) + 's';
        } else {
            return seconds + 's';
        }
    }
    
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

// Initialize dashboard when page loads
document.addEventListener('DOMContentLoaded', () => {
    new Dashboard();
});
`
