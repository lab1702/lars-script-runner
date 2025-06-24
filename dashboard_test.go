package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewDashboardManager tests dashboard manager creation
func TestNewDashboardManager(t *testing.T) {
	config := &Config{
		WebPort: 8080,
		WebHost: "localhost",
	}

	dm := NewDashboardManager(config)

	if dm == nil {
		t.Fatal("Expected non-nil DashboardManager")
	}

	if dm.config != config {
		t.Error("Config not set correctly")
	}

	if dm.processes == nil {
		t.Error("Processes map not initialized")
	}

	if dm.stopUpdates == nil {
		t.Error("stopUpdates channel not initialized")
	}
}

// TestDashboardProcessRegistration tests process registration
func TestDashboardProcessRegistration(t *testing.T) {
	config := &Config{
		GracePeriod:    5 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
		WebPort:        8080,
		WebHost:        "localhost",
	}

	dm := NewDashboardManager(config)

	pm, err := NewProcessManager("echo test", config, "test-process", nil)
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	// Register process
	dm.RegisterProcess(pm)

	// Check if process is registered
	dm.mutex.RLock()
	_, exists := dm.processes["test-process"]
	dm.mutex.RUnlock()

	if !exists {
		t.Error("Process not registered in dashboard")
	}

	// Unregister process
	dm.UnregisterProcess("test-process")

	// Check if process is unregistered
	dm.mutex.RLock()
	_, exists = dm.processes["test-process"]
	dm.mutex.RUnlock()

	if exists {
		t.Error("Process not unregistered from dashboard")
	}
}

// TestDashboardHandlers tests HTTP handlers
func TestDashboardHandlers(t *testing.T) {
	config := &Config{
		WebPort: 8080,
		WebHost: "localhost",
	}

	dm := NewDashboardManager(config)

	// Test dashboard HTML handler
	t.Run("dashboard HTML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		dm.handleDashboard(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Lars Script Runner Dashboard") {
			t.Error("Dashboard HTML does not contain expected title")
		}
	})

	// Test processes API endpoint
	t.Run("processes API", func(t *testing.T) {
		// Add a test process
		pm, err := NewProcessManager("echo test", config, "test-process", nil)
		if err != nil {
			t.Fatalf("Failed to create ProcessManager: %v", err)
		}
		dm.RegisterProcess(pm)

		req := httptest.NewRequest("GET", "/api/processes", nil)
		w := httptest.NewRecorder()

		dm.handleProcesses(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		var processes []interface{}
		err = json.Unmarshal(w.Body.Bytes(), &processes)
		if err != nil {
			t.Errorf("Failed to parse JSON response: %v", err)
		}

		if len(processes) != 1 {
			t.Errorf("Expected 1 process, got %d", len(processes))
		}
	})

	// Test individual process API endpoint
	t.Run("single process API", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/process/test-process", nil)
		w := httptest.NewRecorder()

		dm.handleProcess(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var process map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &process)
		if err != nil {
			t.Errorf("Failed to parse JSON response: %v", err)
		}

		if process["id"] != "test-process" {
			t.Errorf("Expected process ID 'test-process', got %v", process["id"])
		}
	})

	// Test non-existent process
	t.Run("non-existent process", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/process/non-existent", nil)
		w := httptest.NewRecorder()

		dm.handleProcess(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	// Test restart endpoint
	t.Run("restart process", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/restart/test-process", nil)
		w := httptest.NewRecorder()

		dm.handleRestart(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		if err != nil {
			t.Errorf("Failed to parse JSON response: %v", err)
		}

		if response["status"] != "restarted" {
			t.Errorf("Expected status 'restarted', got %v", response["status"])
		}
	})

	// Test restart with wrong method
	t.Run("restart wrong method", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/restart/test-process", nil)
		w := httptest.NewRecorder()

		dm.handleRestart(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status 405, got %d", w.Code)
		}
	})
}

// TestStaticFiles tests static file serving
func TestStaticFiles(t *testing.T) {
	config := &Config{
		WebPort: 8080,
		WebHost: "localhost",
	}

	dm := NewDashboardManager(config)

	tests := []struct {
		path        string
		contentType string
		shouldExist bool
	}{
		{"/static/style.css", "text/css; charset=utf-8", true},
		{"/static/script.js", "application/javascript; charset=utf-8", true},
		{"/static/nonexistent.txt", "", false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", test.path, nil)
			w := httptest.NewRecorder()

			dm.handleStatic(w, req)

			if test.shouldExist {
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200, got %d", w.Code)
				}

				contentType := w.Header().Get("Content-Type")
				if contentType != test.contentType {
					t.Errorf("Expected Content-Type %s, got %s", test.contentType, contentType)
				}

				if w.Body.Len() == 0 {
					t.Error("Expected non-empty response body")
				}
			} else {
				if w.Code != http.StatusNotFound {
					t.Errorf("Expected status 404, got %d", w.Code)
				}
			}
		})
	}
}

// TestProcessStats tests process statistics functionality
func TestProcessStats(t *testing.T) {
	config := &Config{
		GracePeriod:    5 * time.Second,
		RestartDelay:   time.Second,
		MaxRetries:     0,
		BackoffEnabled: true,
	}

	pm, err := NewProcessManager("echo test", config, "test-process", nil)
	if err != nil {
		t.Fatalf("Failed to create ProcessManager: %v", err)
	}

	// Test initial stats
	stats := pm.GetStats()
	if stats.Status != StatusStopped {
		t.Errorf("Expected initial status %s, got %s", StatusStopped, stats.Status)
	}

	if stats.RestartCount != 0 {
		t.Errorf("Expected initial restart count 0, got %d", stats.RestartCount)
	}

	// Test status updates
	pm.UpdateStats(StatusStarting, 0)
	stats = pm.GetStats()
	if stats.Status != StatusStarting {
		t.Errorf("Expected status %s, got %s", StatusStarting, stats.Status)
	}

	if stats.RestartCount != 1 {
		t.Errorf("Expected restart count 1, got %d", stats.RestartCount)
	}

	// Test running status
	pm.UpdateStats(StatusRunning, 12345)
	stats = pm.GetStats()
	if stats.Status != StatusRunning {
		t.Errorf("Expected status %s, got %s", StatusRunning, stats.Status)
	}

	if stats.PID != 12345 {
		t.Errorf("Expected PID 12345, got %d", stats.PID)
	}

	// Test failure status
	pm.UpdateStats(StatusFailed, 0)
	stats = pm.GetStats()
	if stats.Status != StatusFailed {
		t.Errorf("Expected status %s, got %s", StatusFailed, stats.Status)
	}

	if stats.FailureCount != 1 {
		t.Errorf("Expected failure count 1, got %d", stats.FailureCount)
	}

	if stats.LastFailure.IsZero() {
		t.Error("Expected LastFailure to be set")
	}
}

// BenchmarkDashboardHandlers benchmarks the HTTP handlers
func BenchmarkDashboardHandlers(b *testing.B) {
	config := &Config{
		WebPort: 8080,
		WebHost: "localhost",
	}

	dm := NewDashboardManager(config)

	// Add some test processes
	for i := 0; i < 10; i++ {
		pm, _ := NewProcessManager(fmt.Sprintf("echo test%d", i), config, fmt.Sprintf("proc_%d", i), nil)
		dm.RegisterProcess(pm)
	}

	b.Run("processes API", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/api/processes", nil)
			w := httptest.NewRecorder()
			dm.handleProcesses(w, req)
		}
	})

	b.Run("dashboard HTML", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			dm.handleDashboard(w, req)
		}
	})
}
