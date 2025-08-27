package devwatch

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fsnotify/fsnotify"
)

type FakeFileEvent struct{ Called *int32 } // Use int32 for atomic operations

func (f *FakeFileEvent) NewFileEvent(fileName, extension, filePath, event string) error {
	atomic.StoreInt32(f.Called, 1) // Thread-safe write
	return nil
}

type FakeGoFileHandler struct{ Called *int32 } // Use int32 for atomic operations

func (f *FakeGoFileHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	atomic.StoreInt32(f.Called, 1) // Thread-safe write
	return nil
}

func (f *FakeGoFileHandler) MainFilePath() string      { return "fake/main.go" }
func (f *FakeGoFileHandler) UnobservedFiles() []string { return []string{"fake_output.exe"} }

// CountingFileEvent counts how many times NewFileEvent is called
type CountingFileEvent struct {
	mu        sync.Mutex
	CallCount *int
	Calls     *[]string // Store call details for debugging
}

func (f *CountingFileEvent) NewFileEvent(fileName, extension, filePath, event string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.CallCount++
	*f.Calls = append(*f.Calls, fileName+" "+extension+" "+event)
	return nil
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
	countingEvent := &CountingFileEvent{CallCount: assetCallCount, Calls: assetCalls}
	config := &WatchConfig{
		AppRootDir:      tempDir,
		FileEventAssets: countingEvent,
		FilesEventGO:    []GoFileHandler{},           // Empty for this test
		BrowserReload:   func() error { return nil }, // No browser reload needed for this test
		Logger:          os.Stdout,
		ExitChan:        make(chan bool, 1),
	}
	w := New(config)
	w.supportedAssetsExtensions = []string{".html", ".css", ".js"}
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
	config := &WatchConfig{
		AppRootDir:      tempDir,
		FileEventAssets: &FakeFileEvent{Called: assetCalled},
		FilesEventGO:    []GoFileHandler{&FakeGoFileHandler{Called: goCalled}},
		BrowserReload: func() error {
			atomic.AddInt64(reloadCount, 1) // Thread-safe increment
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   os.Stdout,
		ExitChan: make(chan bool, 1),
	}
	w := New(config)
	w.supportedAssetsExtensions = []string{".css"}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	return w, watcher
}
