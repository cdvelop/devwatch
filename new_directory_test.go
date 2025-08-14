package devwatch

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewDirectoryAfterInitialization tests that DevWatch properly handles
// new directories created after initialization and watches for file changes inside them
func TestNewDirectoryAfterInitialization(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "devwatch_new_dir_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set up tracking variables
	assetCallCount := 0
	assetCalls := []string{}
	var goEventCalled int32 // Use int32 for atomic operations
	var reloadCount int64   // Use atomic for thread-safe access
	reloadCalled := make(chan struct{}, 10)

	// Create DevWatch instance using helper
	dw, watcher, countingEvent := NewTestDevWatchForDuplication(t, tempDir, &assetCallCount, &assetCalls)
	defer watcher.Close()

	// Add Go handler for comprehensive testing
	fakeGoHandler := &FakeGoFileHandler{Called: &goEventCalled}
	dw.FilesEventGO = []GoFileHandler{fakeGoHandler}
	dw.BrowserReload = func() error {
		atomic.AddInt64(&reloadCount, 1) // Thread-safe increment
		select {
		case reloadCalled <- struct{}{}:
		default:
		}
		return nil
	}

	// Create initial go.mod file
	goModContent := "module testmodule\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Perform initial registration
	dw.InitialRegistration()

	// Start watching in a goroutine
	go dw.watchEvents()

	// Allow some time for the watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// 1. Create a new directory AFTER initialization
	newDirPath := filepath.Join(tempDir, "newdir")
	if err := os.Mkdir(newDirPath, 0755); err != nil {
		t.Fatalf("failed to create new directory: %v", err)
	}

	// Wait for the directory creation to be processed
	time.Sleep(200 * time.Millisecond)

	// Reset counters to focus on events in the new directory
	initialCount, initialCalls := countingEvent.GetCounts()
	t.Logf("Initial events count: %d, calls: %v", initialCount, initialCalls)

	// 2. Create a CSS file inside the new directory
	cssFilePath := filepath.Join(newDirPath, "style.css")
	cssContent := "body { color: red; }"
	if err := os.WriteFile(cssFilePath, []byte(cssContent), 0644); err != nil {
		t.Fatalf("failed to create CSS file: %v", err)
	}

	// Wait for file creation to be processed
	time.Sleep(200 * time.Millisecond)

	// 3. Modify the CSS file
	modifiedCssContent := "body { color: blue; background: white; }"
	if err := os.WriteFile(cssFilePath, []byte(modifiedCssContent), 0644); err != nil {
		t.Fatalf("failed to modify CSS file: %v", err)
	}

	// Wait for file modification to be processed
	time.Sleep(200 * time.Millisecond)

	// 4. Create a Go file inside the new directory
	goFilePath := filepath.Join(newDirPath, "test.go")
	goContent := "package main\n\nfunc main() {\n\tfmt.Println(\"Hello from new directory\")\n}"
	if err := os.WriteFile(goFilePath, []byte(goContent), 0644); err != nil {
		t.Fatalf("failed to create Go file: %v", err)
	}

	// Wait for Go file creation to be processed
	time.Sleep(200 * time.Millisecond)

	// Check the results
	finalCount, finalCalls := countingEvent.GetCounts()
	t.Logf("Final events count: %d, calls: %v", finalCount, finalCalls)

	// Stop the watcher
	dw.ExitChan <- true

	// Verify that events were detected for files in the new directory
	newDirEvents := 0
	for _, call := range finalCalls {
		if call == "style.css .css create" || call == "style.css .css write" {
			newDirEvents++
		}
	}

	// Test assertions
	if newDirEvents == 0 {
		t.Errorf("Expected CSS file events in new directory, but got none. All calls: %v", finalCalls)
		t.Error("This indicates that the new directory is not being watched for file changes")
	}

	// Additional verification: Check if the new directory was actually added to the watcher
	// We can't directly access watcher.WatchList(), but we can test the behavior
	t.Logf("Asset event called: %t", finalCount > initialCount)
	t.Logf("Go event called: %t", atomic.LoadInt32(&goEventCalled) > 0) // Thread-safe read
	t.Logf("Browser reload count: %d", atomic.LoadInt64(&reloadCount))  // Thread-safe read

	if finalCount <= initialCount {
		t.Error("Expected new file events after creating files in new directory")
	}
}

// TestDirectoryCreationWithSubdirectories tests nested directory creation
func TestDirectoryCreationWithSubdirectories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "devwatch_nested_dir_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	assetCallCount := 0
	assetCalls := []string{}

	dw, watcher, _ := NewTestDevWatchForDuplication(t, tempDir, &assetCallCount, &assetCalls)
	defer watcher.Close()

	// Create initial go.mod
	goModContent := "module testmodule\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	dw.InitialRegistration()
	go dw.watchEvents()
	time.Sleep(100 * time.Millisecond)

	// Create nested directories
	nestedPath := filepath.Join(tempDir, "level1", "level2", "level3")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("failed to create nested directories: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Create a file in the deepest directory
	cssFile := filepath.Join(nestedPath, "deep.css")
	if err := os.WriteFile(cssFile, []byte("/* deep css */"), 0644); err != nil {
		t.Fatalf("failed to create deep CSS file: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	dw.ExitChan <- true

	// Check if the file in nested directory was detected using thread-safe access
	_, calls := dw.FileEventAssets.(*CountingFileEvent).GetCounts()
	t.Logf("Asset calls: %v", calls)

	deepFileDetected := false
	for _, call := range calls {
		if call == "deep.css .css create" || call == "deep.css .css write" {
			deepFileDetected = true
			break
		}
	}

	if !deepFileDetected {
		t.Error("File in deeply nested directory was not detected - indicating nested directories are not being watched")
	}
}

// TestRapidDirectoryAndFileCreation tests rapid creation of directories and files
func TestRapidDirectoryAndFileCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "devwatch_rapid_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	assetCallCount := 0
	assetCalls := []string{}

	dw, watcher, countingEvent := NewTestDevWatchForDuplication(t, tempDir, &assetCallCount, &assetCalls)
	defer watcher.Close()

	// Create initial go.mod
	goModContent := "module testmodule\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	dw.InitialRegistration()
	go dw.watchEvents()
	time.Sleep(100 * time.Millisecond)

	// Rapid creation: directory -> file -> modify
	rapidDir := filepath.Join(tempDir, "rapid")
	if err := os.Mkdir(rapidDir, 0755); err != nil {
		t.Fatalf("failed to create rapid directory: %v", err)
	}

	// Give a small delay to ensure the directory is added to the watcher
	// before creating files inside it
	time.Sleep(150 * time.Millisecond)

	// Immediately create a file (this often fails if directory isn't being watched)
	rapidFile := filepath.Join(rapidDir, "rapid.js")
	if err := os.WriteFile(rapidFile, []byte("console.log('rapid');"), 0644); err != nil {
		t.Fatalf("failed to create rapid file: %v", err)
	}

	// Immediate modification
	if err := os.WriteFile(rapidFile, []byte("console.log('rapid modified');"), 0644); err != nil {
		t.Fatalf("failed to modify rapid file: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	dw.ExitChan <- true

	_, calls := countingEvent.GetCounts()
	t.Logf("Rapid test calls: %v", calls)

	// Should detect the JavaScript file events
	jsDetected := false
	for _, call := range calls {
		if call == "rapid.js .js create" || call == "rapid.js .js write" {
			jsDetected = true
			break
		}
	}

	if !jsDetected {
		t.Error("Rapid file creation in new directory was not detected")
	}
}
