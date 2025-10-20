package devwatch

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// CountingCompilingHandler counts compilations
type CountingCompilingHandler struct {
	compilationCount     *int32
	compilationDuration  time.Duration
	SupportedExtensions_ []string
	MainInputFile        string
}

func (h *CountingCompilingHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	atomic.AddInt32(h.compilationCount, 1)
	time.Sleep(h.compilationDuration)
	return nil
}

func (h *CountingCompilingHandler) SupportedExtensions() []string {
	return h.SupportedExtensions_
}

func (h *CountingCompilingHandler) MainInputFileRelativePath() string {
	if h.MainInputFile == "" {
		return "src/cmd/webclient/main.go"
	}
	return h.MainInputFile
}

func (h *CountingCompilingHandler) UnobservedFiles() []string {
	return []string{"main.wasm"}
}

// TestWasmReloadRaceCondition_RapidEdits tests the bug where rapid edits
// are ignored due to 1-second debouncing, causing browser to reload without
// recompiling
func TestWasmReloadRaceCondition_RapidEdits(t *testing.T) {
	tempDir := t.TempDir()

	// Create project structure
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

	// Counters to track compilations
	var compilationCount int32
	var reloadCount int64

	// Handler that tracks each compilation
	wasmHandler := &CountingCompilingHandler{
		compilationCount:     &compilationCount,
		compilationDuration:  100 * time.Millisecond, // Fast compilation
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/cmd/webclient/main.go",
	}

	reloadCalled := make(chan struct{}, 10)

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

	// Simulate rapid edits
	t.Log("=== Simulating 3 rapid edits ===")

	// Edit 1 at t=0
	t.Log("Edit 1: t=0ms")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for first reload
	select {
	case <-reloadCalled:
		t.Log("Edit 1: Browser reloaded")
	case <-time.After(1 * time.Second):
		t.Fatal("Edit 1: Browser reload timeout")
	}

	// Edit 2 at t=300ms (within 1 second debounce window)
	time.Sleep(300 * time.Millisecond)
	t.Log("Edit 2: t=300ms (within 1s debounce)")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait to see if compilation happens
	time.Sleep(200 * time.Millisecond)

	// Edit 3 at t=600ms (still within 1 second from edit 1)
	time.Sleep(100 * time.Millisecond)
	t.Log("Edit 3: t=600ms (within 1s debounce)")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for any pending operations
	time.Sleep(500 * time.Millisecond)

	// Cleanup
	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	// Analyze results
	compilations := atomic.LoadInt32(&compilationCount)
	reloads := atomic.LoadInt64(&reloadCount)

	t.Log("\n=== Results ===")
	t.Logf("Total edits sent: 3")
	t.Logf("Compilations executed: %d", compilations)
	t.Logf("Browser reloads: %d", reloads)

	// Expected: 3 compilations (one per edit)
	// Bug: Only 1 compilation (edits 2 and 3 ignored due to debounce)
	if compilations < 3 {
		t.Errorf("❌ BUG CONFIRMED: Only %d compilations for 3 edits!", compilations)
		t.Errorf("   Edits 2 and 3 were IGNORED due to 1-second debounce")
		t.Errorf("   This means browser reloaded with STALE code!")
	} else {
		t.Logf("✅ CORRECT: All 3 edits triggered compilation")
	}
}

// TestWasmReloadRaceCondition_SlowCompilationRapidEdits tests the worst case:
// slow compilation + rapid edits
func TestWasmReloadRaceCondition_SlowCompilationRapidEdits(t *testing.T) {
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
func main() { println("test") }
`
	if err := os.WriteFile(mainGoFile, []byte(mainGoContent), 0644); err != nil {
		t.Fatal(err)
	}

	var compilationCount int32
	var reloadCount int64

	// Slow compilation (500ms) simulates real WASM compilation
	wasmHandler := &CountingCompilingHandler{
		compilationCount:     &compilationCount,
		compilationDuration:  500 * time.Millisecond,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "src/cmd/webclient/main.go",
	}

	reloadCalled := make(chan struct{}, 10)

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{wasmHandler},
		BrowserReload: func() error {
			count := atomic.AddInt64(&reloadCount, 1)
			t.Logf("Browser reload %d", count)
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

	t.Log("=== Edit 1 (should compile) ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait 200ms (compilation still running)
	time.Sleep(200 * time.Millisecond)

	// Edit while compilation is in progress
	t.Log("=== Edit 2 at t=200ms (compilation in progress) ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for first compilation to finish
	time.Sleep(500 * time.Millisecond)

	// Edit after first compilation
	t.Log("=== Edit 3 at t=700ms ===")
	watcher.Events <- fsnotify.Event{
		Name: mainGoFile,
		Op:   fsnotify.Write,
	}

	// Wait for operations
	time.Sleep(1 * time.Second)

	w.ExitChan <- true
	time.Sleep(100 * time.Millisecond)

	compilations := atomic.LoadInt32(&compilationCount)
	reloads := atomic.LoadInt64(&reloadCount)

	t.Log("\n=== Results ===")
	t.Logf("Total edits: 3")
	t.Logf("Compilations: %d", compilations)
	t.Logf("Reloads: %d", reloads)

	// The critical question: did edit 2 trigger compilation?
	if compilations < 2 {
		t.Errorf("❌ CRITICAL BUG: Edit during compilation was LOST!")
		t.Errorf("   Only %d compilation(s) for 3 edits", compilations)
	}
}
