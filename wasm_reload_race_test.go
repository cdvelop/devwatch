package devwatch

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SlowCompilingHandler simulates a WASM handler with slow compilation
// This reproduces the real-world scenario where compilation takes time
type SlowCompilingHandler struct {
	compilationStarted   *int32
	compilationCompleted *int32
	compilationEndTime   *time.Time
	compilationDuration  time.Duration
	SupportedExtensions_ []string
	MainInputFile        string
}

func (h *SlowCompilingHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	// Mark compilation as started
	atomic.StoreInt32(h.compilationStarted, 1)

	// Simulate slow compilation (like WASM compilation)
	time.Sleep(h.compilationDuration)

	// Mark compilation as completed and record exact time
	atomic.StoreInt32(h.compilationCompleted, 1)
	if h.compilationEndTime != nil {
		*h.compilationEndTime = time.Now()
	}

	return nil
}

func (h *SlowCompilingHandler) SupportedExtensions() []string {
	return h.SupportedExtensions_
}

func (h *SlowCompilingHandler) MainInputFileRelativePath() string {
	if h.MainInputFile == "" {
		return "src/cmd/webclient/main.go"
	}
	return h.MainInputFile
}

func (h *SlowCompilingHandler) UnobservedFiles() []string {
	return []string{"main.wasm"}
}

// ErrorCompilingHandler simulates a handler that fails during compilation
type ErrorCompilingHandler struct {
	SupportedExtensions_ []string
	MainInputFile        string
}

func (h *ErrorCompilingHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	return os.ErrInvalid // Simulate compilation error
}

func (h *ErrorCompilingHandler) SupportedExtensions() []string {
	return h.SupportedExtensions_
}

func (h *ErrorCompilingHandler) MainInputFileRelativePath() string {
	if h.MainInputFile == "" {
		return "src/cmd/webclient/main.go"
	}
	return h.MainInputFile
}

func (h *ErrorCompilingHandler) UnobservedFiles() []string {
	return []string{"main.wasm"}
}

// TestWasmReloadRaceCondition reproduces the bug where browser reloads
// before WASM compilation completes
func TestWasmReloadRaceCondition(t *testing.T) {
	tempDir := t.TempDir()

	// Create a minimal Go project structure
	err := os.MkdirAll(tempDir+"/src/cmd/webclient", 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create go.mod
	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main.go
	mainGoFile := tempDir + "/src/cmd/webclient/main.go"
	mainGoContent := `package main

import "syscall/js"

func main() {
	dom := js.Global().Get("document").Call("createElement", "div")
	dom.Set("innerHTML", "Hello, WebAssembly! 0")
	body := js.Global().Get("document").Get("body")
	body.Call("appendChild", dom)
	select {}
}
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Counters to track events
	var compilationStarted int32
	var compilationCompleted int32
	var reloadCount int64
	var browserReloadTime time.Time
	var compilationEndTime time.Time
	reloadCalled := make(chan struct{}, 10)

	// Create handler that simulates WASM compilation (takes 200ms)
	wasmHandler := &SlowCompilingHandler{
		compilationStarted:   &compilationStarted,
		compilationCompleted: &compilationCompleted,
		compilationEndTime:   &compilationEndTime,
		compilationDuration:  200 * time.Millisecond, // Realistic compilation time
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/cmd/webclient/main.go",
	}

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{wasmHandler},
		BrowserReload: func() error {
			browserReloadTime = time.Now()
			atomic.AddInt64(&reloadCount, 1)
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

	// Start watching events
	go w.watchEvents()

	// Give watchEvents time to initialize
	time.Sleep(100 * time.Millisecond)

	// Simulate file edit event
	t.Log("Simulating file edit event...")
	editStartTime := time.Now()
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for browser reload to be called
	select {
	case <-reloadCalled:
		t.Log("Browser reload was triggered")
	case <-time.After(2 * time.Second):
		t.Fatal("Browser reload was not triggered within timeout")
	}

	// Wait a bit for any pending operations
	time.Sleep(50 * time.Millisecond)

	// Cleanup
	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	// Analyze the race condition
	t.Log("\n=== Race Condition Analysis ===")
	t.Logf("Edit started at: %v", editStartTime)
	t.Logf("Browser reload at: %v (after %v)", browserReloadTime, browserReloadTime.Sub(editStartTime))
	if !compilationEndTime.IsZero() {
		t.Logf("Compilation ended at: %v (after %v)", compilationEndTime, compilationEndTime.Sub(editStartTime))
	}

	// Check if compilation was started
	if atomic.LoadInt32(&compilationStarted) == 0 {
		t.Fatal("❌ BUG CONFIRMED: Compilation was never started!")
	}

	// Check if reload happened before compilation completed
	compilationComplete := atomic.LoadInt32(&compilationCompleted)
	reloads := atomic.LoadInt64(&reloadCount)

	t.Logf("Reload count: %d", reloads)
	t.Logf("Compilation completed: %v", compilationComplete == 1)

	// THE BUG: Browser reloads before compilation completes
	if compilationComplete == 0 && reloads > 0 {
		t.Errorf("❌ BUG CONFIRMED: Browser reloaded BEFORE compilation completed!")
		t.Errorf("   - Compilation started: YES")
		t.Errorf("   - Compilation completed: NO")
		t.Errorf("   - Browser reloads: %d", reloads)
		t.Errorf("   This means the browser loaded stale WASM code!")
	}

	// Expected behavior: reload should wait for compilation
	if compilationComplete == 1 && reloads > 0 {
		reloadDelay := browserReloadTime.Sub(compilationEndTime)

		// The actual flow is:
		// 1. File event triggers compilation (blocks for compilationDuration)
		// 2. After compilation completes, scheduleReload() is called
		// 3. scheduleReload() programs a 50ms timer
		// 4. After 50ms, browser reloads
		//
		// So the expected delay is: ~50ms (debounce time)
		// A negative delay or very small delay would indicate a race condition

		if reloadDelay < 0 {
			t.Errorf("❌ BUG CONFIRMED: Browser reloaded %v BEFORE compilation ended!", -reloadDelay)
		} else if reloadDelay < 10*time.Millisecond {
			// If delay is less than 10ms, the timer might have been programmed during compilation
			t.Errorf("❌ POSSIBLE RACE: Browser reloaded only %v after compilation (expected ~50ms debounce)", reloadDelay)
		} else {
			t.Logf("✅ CORRECT: Browser reloaded %v AFTER compilation ended (debounce working)", reloadDelay)
		}
	}
}

// TestWasmReloadRaceCondition_FastCompilation tests that fast compilations
// work correctly (control test)
func TestWasmReloadRaceCondition_FastCompilation(t *testing.T) {
	tempDir := t.TempDir()

	// Create a minimal Go project structure
	err := os.MkdirAll(tempDir+"/src/cmd/webclient", 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create go.mod
	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main.go
	mainGoFile := tempDir + "/src/cmd/webclient/main.go"
	mainGoContent := `package main

func main() {
	println("fast compilation")
}
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Counters
	var compilationStarted int32
	var compilationCompleted int32
	var reloadCount int64
	reloadCalled := make(chan struct{}, 10)

	// Fast compilation (10ms)
	wasmHandler := &SlowCompilingHandler{
		compilationStarted:   &compilationStarted,
		compilationCompleted: &compilationCompleted,
		compilationDuration:  10 * time.Millisecond,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/cmd/webclient/main.go",
	}

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{wasmHandler},
		BrowserReload: func() error {
			atomic.AddInt64(&reloadCount, 1)
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
	time.Sleep(100 * time.Millisecond)

	// Simulate file edit
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for reload
	select {
	case <-reloadCalled:
		t.Log("Browser reload triggered")
	case <-time.After(2 * time.Second):
		t.Fatal("Browser reload not triggered")
	}

	// Cleanup
	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	// With fast compilation, both should be done
	if atomic.LoadInt32(&compilationCompleted) == 1 && atomic.LoadInt64(&reloadCount) > 0 {
		t.Log("✅ Fast compilation completed before reload (expected)")
	}
}

// TestWasmReloadRaceCondition_CompilationError tests that compilation errors
// prevent browser reload
func TestWasmReloadRaceCondition_CompilationError(t *testing.T) {
	tempDir := t.TempDir()

	err := os.MkdirAll(tempDir+"/src/cmd/webclient", 0755)
	if err != nil {
		t.Fatal(err)
	}

	goModContent := "module example\n\ngo 1.25.2\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainGoFile := tempDir + "/src/cmd/webclient/main.go"
	mainGoContent := `package main

func main() {
	// This will cause a syntax error when we edit it
}
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	var reloadCount int64
	reloadCalled := make(chan struct{}, 10)

	// Handler that returns an error (simulating compilation failure)
	errorHandler := &ErrorCompilingHandler{
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/cmd/webclient/main.go",
	}

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{errorHandler},
		BrowserReload: func() error {
			atomic.AddInt64(&reloadCount, 1)
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
	time.Sleep(100 * time.Millisecond)

	// Simulate file edit with error
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait to see if reload happens (it shouldn't)
	select {
	case <-reloadCalled:
		t.Error("❌ BUG: Browser reloaded despite compilation error!")
	case <-time.After(500 * time.Millisecond):
		t.Log("✅ CORRECT: Browser did not reload due to compilation error")
	}

	// Cleanup
	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	// Verify no reloads occurred
	if atomic.LoadInt64(&reloadCount) > 0 {
		t.Errorf("Browser reload count: %d (expected 0 due to error)", reloadCount)
	}
}
