package devwatch

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ErrorHandler simulates a handler that always fails (e.g., compilation error)
type ErrorHandler struct {
	callCount            *int32
	SupportedExtensions_ []string
	MainInputFile        string
}

func (h *ErrorHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	atomic.AddInt32(h.callCount, 1)
	return errors.New("compilation failed: syntax error")
}

func (h *ErrorHandler) SupportedExtensions() []string {
	return h.SupportedExtensions_
}

func (h *ErrorHandler) MainInputFileRelativePath() string {
	return h.MainInputFile
}

func (h *ErrorHandler) UnobservedFiles() []string {
	return nil
}

// SuccessHandler simulates a handler that always succeeds
type SuccessHandler struct {
	callCount            *int32
	SupportedExtensions_ []string
	MainInputFile        string
}

func (h *SuccessHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	atomic.AddInt32(h.callCount, 1)
	return nil // Success
}

func (h *SuccessHandler) SupportedExtensions() []string {
	return h.SupportedExtensions_
}

func (h *SuccessHandler) MainInputFileRelativePath() string {
	return h.MainInputFile
}

func (h *SuccessHandler) UnobservedFiles() []string {
	return nil
}

// TestMultipleHandlers_FirstHandlerError_SecondHandlerSuccess tests the scenario where:
// 1. First Go handler returns an error (e.g., compilation failure)
// 2. Second Go handler succeeds (e.g., different task like linting succeeds)
// 3. Expected: Browser should reload because second handler succeeded
// 4. Actual BUG: Browser doesn't reload because first handler's error blocks it
func TestMultipleHandlers_FirstHandlerError_SecondHandlerSuccess(t *testing.T) {
	tempDir := t.TempDir()

	err := os.MkdirAll(tempDir+"/src", 0755)
	if err != nil {
		t.Fatal(err)
	}

	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Single main.go file that BOTH handlers will process
	mainGoFile := tempDir + "/src/main.go"
	mainGoContent := `package main
func main() { println("test") }
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	var handler1Calls int32
	var handler2Calls int32
	var reloadCount int64

	// Handler 1: Always fails (simulates compilation error)
	errorHandler := &ErrorHandler{
		callCount:            &handler1Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	// Handler 2: Always succeeds (simulates successful linting or other task)
	successHandler := &SuccessHandler{
		callCount:            &handler2Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	reloadCalled := make(chan struct{}, 10)

	config := &WatchConfig{
		AppRootDir: tempDir,
		// IMPORTANT: Order matters - error handler first, success handler second
		FilesEventHandlers: []FilesEventHandlers{errorHandler, successHandler},
		BrowserReload: func() error {
			count := atomic.AddInt64(&reloadCount, 1)
			t.Logf("✓ Browser reload called (count: %d)", count)
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   func(message ...any) { t.Log(message...) },
		ExitChan: make(chan bool, 1),
	}

	w := New(config)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	defer watcher.Close()

	go w.watchEvents()
	time.Sleep(50 * time.Millisecond)

	// Edit the shared file (both handlers will be called)
	t.Log("\n=== EDIT: Modifying src/main.go (both handlers process it) ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for potential reload
	select {
	case <-reloadCalled:
		t.Log("✓ Browser reload happened (EXPECTED)")
	case <-time.After(500 * time.Millisecond):
		t.Log("✗ Browser reload DID NOT happen (BUG!)")
	}

	time.Sleep(100 * time.Millisecond)

	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	handler1CallCount := atomic.LoadInt32(&handler1Calls)
	handler2CallCount := atomic.LoadInt32(&handler2Calls)
	reloads := atomic.LoadInt64(&reloadCount)

	t.Log("\n=== RESULTS ===")
	t.Logf("Handler 1 (Error) calls: %d", handler1CallCount)
	t.Logf("Handler 2 (Success) calls: %d", handler2CallCount)
	t.Logf("Browser reloads: %d", reloads)

	// Verify both handlers were called
	if handler1CallCount != 1 {
		t.Errorf("Handler 1 should have been called 1 time, got %d", handler1CallCount)
	}
	if handler2CallCount != 1 {
		t.Errorf("Handler 2 should have been called 1 time, got %d", handler2CallCount)
	}

	// This is the BUG: Even though handler 2 succeeded, browser doesn't reload
	// because handler 1's error set goHandlerError
	if reloads != 1 {
		t.Errorf("❌ BUG REPRODUCED: Expected 1 reload (handler 2 succeeded), got %d", reloads)
		t.Errorf("   Root cause: goHandlerError from handler 1 blocks reload even though handler 2 succeeded")
		t.Errorf("   Fix needed: Only block reload if ALL handlers fail, not if ANY handler fails")
	} else {
		t.Log("✅ FIXED: Browser reloaded despite handler 1 error (handler 2 succeeded)")
	}
}

// TestMultipleHandlers_BothHandlersFail tests that reload should NOT happen
// when ALL handlers fail
func TestMultipleHandlers_BothHandlersFail(t *testing.T) {
	tempDir := t.TempDir()

	err := os.MkdirAll(tempDir+"/src", 0755)
	if err != nil {
		t.Fatal(err)
	}

	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainGoFile := tempDir + "/src/main.go"
	mainGoContent := `package main
func main() { println("test") }
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	var handler1Calls int32
	var handler2Calls int32
	var reloadCount int64

	// Both handlers fail
	errorHandler1 := &ErrorHandler{
		callCount:            &handler1Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	errorHandler2 := &ErrorHandler{
		callCount:            &handler2Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	reloadCalled := make(chan struct{}, 10)

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{errorHandler1, errorHandler2},
		BrowserReload: func() error {
			atomic.AddInt64(&reloadCount, 1)
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   func(message ...any) { /* Silent */ },
		ExitChan: make(chan bool, 1),
	}

	w := New(config)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	defer watcher.Close()

	go w.watchEvents()
	time.Sleep(50 * time.Millisecond)

	t.Log("=== EDIT: Modifying file (both handlers will fail) ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait to see if reload happens (it shouldn't)
	select {
	case <-reloadCalled:
		t.Error("✗ Browser reload happened but SHOULD NOT (all handlers failed)")
	case <-time.After(500 * time.Millisecond):
		t.Log("✓ Browser reload correctly did NOT happen (all handlers failed)")
	}

	time.Sleep(100 * time.Millisecond)

	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	reloads := atomic.LoadInt64(&reloadCount)

	if reloads != 0 {
		t.Errorf("Expected 0 reloads (all handlers failed), got %d", reloads)
	} else {
		t.Log("✅ CORRECT: No reload when all handlers fail")
	}
}

// TestMultipleHandlers_BothHandlersSucceed tests that reload SHOULD happen
// when ALL handlers succeed
func TestMultipleHandlers_BothHandlersSucceed(t *testing.T) {
	tempDir := t.TempDir()

	err := os.MkdirAll(tempDir+"/src", 0755)
	if err != nil {
		t.Fatal(err)
	}

	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainGoFile := tempDir + "/src/main.go"
	mainGoContent := `package main
func main() { println("test") }
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	var handler1Calls int32
	var handler2Calls int32
	var reloadCount int64

	// Both handlers succeed
	successHandler1 := &SuccessHandler{
		callCount:            &handler1Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	successHandler2 := &SuccessHandler{
		callCount:            &handler2Calls,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/main.go",
	}

	reloadCalled := make(chan struct{}, 10)

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{successHandler1, successHandler2},
		BrowserReload: func() error {
			atomic.AddInt64(&reloadCount, 1)
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   func(message ...any) { /* Silent */ },
		ExitChan: make(chan bool, 1),
	}

	w := New(config)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	defer watcher.Close()

	go w.watchEvents()
	time.Sleep(50 * time.Millisecond)

	t.Log("=== EDIT: Modifying file (both handlers will succeed) ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for reload
	select {
	case <-reloadCalled:
		t.Log("✓ Browser reload happened (EXPECTED)")
	case <-time.After(500 * time.Millisecond):
		t.Error("✗ Browser reload DID NOT happen (should have)")
	}

	time.Sleep(100 * time.Millisecond)

	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	reloads := atomic.LoadInt64(&reloadCount)

	if reloads != 1 {
		t.Errorf("Expected 1 reload (all handlers succeeded), got %d", reloads)
	} else {
		t.Log("✅ CORRECT: Reload when all handlers succeed")
	}
}
