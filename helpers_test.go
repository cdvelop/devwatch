package devwatch

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// FakeFilesEventHandler is a mock implementation of the FilesEventHandlers interface for testing.
type FakeFilesEventHandler struct {
	Called               *int32
	SupportedExtensions_ []string
	MainInputFile        string
	Unobserved           []string
}

func (f *FakeFilesEventHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	if f.Called != nil {
		atomic.StoreInt32(f.Called, 1)
	}
	return nil
}

func (f *FakeFilesEventHandler) SupportedExtensions() []string {
	if f.SupportedExtensions_ == nil {
		return []string{}
	}
	return f.SupportedExtensions_
}

func (f *FakeFilesEventHandler) MainInputFileRelativePath() string {
	if f.MainInputFile == "" {
		return "fake/main.go"
	}
	return f.MainInputFile
}

func (f *FakeFilesEventHandler) UnobservedFiles() []string {
	if f.Unobserved == nil {
		return []string{}
	}
	return f.Unobserved
}

// CountingFileEvent counts how many times NewFileEvent is called
type CountingFileEvent struct {
	mu                   sync.Mutex
	CallCount            *int
	Calls                *[]string // Store call details for debugging
	SupportedExtensions_ []string
}

func (f *CountingFileEvent) NewFileEvent(fileName, extension, filePath, event string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.CallCount++
	*f.Calls = append(*f.Calls, fileName+" "+extension+" "+event)
	return nil
}

func (f *CountingFileEvent) SupportedExtensions() []string {
	return f.SupportedExtensions_
}

func (f *CountingFileEvent) MainInputFileRelativePath() string {
	return "" // Not used in duplication tests
}

func (f *CountingFileEvent) UnobservedFiles() []string {
	return nil // Not used in duplication tests
}

// GetCounts safely returns the current count and calls
func (f *CountingFileEvent) GetCounts() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.CallCount, append([]string{}, *f.Calls...)
}

// Reset safely resets the counters and calls
func (f *CountingFileEvent) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.CallCount = 0
	*f.Calls = (*f.Calls)[:0] // Clear slice but keep underlying array
}

// Helper to create a DevWatch instance for duplication tests
func NewTestDevWatchForDuplication(t *testing.T, tempDir string, assetCallCount *int, assetCalls *[]string) (*DevWatch, *fsnotify.Watcher, *CountingFileEvent) {
	countingEvent := &CountingFileEvent{
		CallCount:            assetCallCount,
		Calls:                assetCalls,
		SupportedExtensions_: []string{".html", ".css", ".js"},
	}
	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{countingEvent},
		BrowserReload:      func() error { return nil }, // No browser reload needed for this test
		Logger:             func(message ...any) { fmt.Println(message...) },
		ExitChan:           make(chan bool, 1),
	}
	w := New(config)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	return w, watcher, countingEvent
}

// Helper to create temp files and go.mod for tests
func CreateTestFiles(t *testing.T, tempDir string) (cssFile, goFile string) {
	cssFile = tempDir + "/file.css"
	goFile = tempDir + "/file.go"
	goModContent := "module testmodule\n\ngo 1.21\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cssFile, []byte("body {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goFile, []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}
	return
}

// Helper to create a DevWatch instance for tests
func NewTestDevWatch(t *testing.T, tempDir string, assetCalled, goCalled *int32, reloadCount *int64, reloadCalled chan struct{}) (*DevWatch, *fsnotify.Watcher) {
	assetHandler := &FakeFilesEventHandler{
		Called:               assetCalled,
		SupportedExtensions_: []string{".css"},
	}
	goHandler := &FakeFilesEventHandler{
		Called:               goCalled,
		SupportedExtensions_: []string{".go"},
		MainInputFile:        "fake/main.go",
	}

	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{assetHandler, goHandler},
		BrowserReload: func() error {
			atomic.AddInt64(reloadCount, 1) // Thread-safe increment
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   func(message ...any) { fmt.Println(message...) },
		ExitChan: make(chan bool, 1),
	}
	w := New(config)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	return w, watcher
}
