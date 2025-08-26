package devwatch

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Test that a .js write event triggers BrowserReload via the watcher
func TestWatchEvents_JSBrowserReloadCalled(t *testing.T) {
	tempDir := t.TempDir()

	// Create a JS file so os.Stat succeeds in the watcher
	jsFile := tempDir + "/app/script.js"
	if err := os.MkdirAll(tempDir+"/app", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsFile, []byte("console.log('hello');"), 0644); err != nil {
		t.Fatal(err)
	}

	// Tracking variables
	var assetCalled int32
	var reloadCount int64
	reloadCalled := make(chan struct{}, 1)

	// Reuse TrackingFileEvent from watchEvents_test.go for asset calls tracking
	eventTracker := &EventTracker{}

	config := &WatchConfig{
		AppRootDir:      tempDir,
		FileEventAssets: &TrackingFileEvent{Tracker: eventTracker, Called: &assetCalled},
		FilesEventGO:    []GoFileHandler{},
		BrowserReload: func() error {
			atomic.AddInt64(&reloadCount, 1)
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   os.Stdout,
		ExitChan: make(chan bool, 1),
	}

	w := New(config)
	// Ensure .js is considered an asset for this test
	w.supportedAssetsExtensions = []string{".js"}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher

	done := make(chan bool)
	go func() {
		w.watchEvents()
		done <- true
	}()

	// send a write event for the JS file
	go func() {
		time.Sleep(10 * time.Millisecond)
		watcher.Events <- fsnotify.Event{Name: jsFile, Op: fsnotify.Write}
		time.Sleep(50 * time.Millisecond)
		w.ExitChan <- true
	}()

	// Wait for at least one reload call
	select {
	case <-reloadCalled:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for BrowserReload to be called for .js write event")
	}

	select {
	case <-done:
		// finished
	case <-time.After(1 * time.Second):
		t.Fatal("watchEvents did not exit in time")
	}

	if atomic.LoadInt32(&assetCalled) == 0 {
		t.Error("Asset handler was not called for .js write event")
	}
	if atomic.LoadInt64(&reloadCount) == 0 {
		t.Error("BrowserReload was not called for .js write event")
	}
}
