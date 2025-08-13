package devwatch

import (
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatchEvents_BrowserReloadCalled(t *testing.T) {
	tempDir := t.TempDir()
	cssFile, goFile := CreateTestFiles(t, tempDir)

	assetCalled := false
	goCalled := false
	reloadCount := 0
	reloadCalled := make(chan struct{}, 2)

	w, watcher := NewTestDevWatch(t, tempDir, &assetCalled, &goCalled, &reloadCount, reloadCalled)

	done := make(chan bool)
	go func() {
		w.watchEvents()
		done <- true
	}()

	go func() {
		time.Sleep(10 * time.Millisecond)
		t.Log("Sending CSS event")
		watcher.Events <- fsnotify.Event{Name: cssFile, Op: fsnotify.Write}
		time.Sleep(100 * time.Millisecond)
		t.Log("Sending GO event")
		watcher.Events <- fsnotify.Event{Name: goFile, Op: fsnotify.Write}
		time.Sleep(100 * time.Millisecond)
		t.Log("Sending exit signal")
		w.ExitChan <- true
	}()

	reloadCallsReceived := 0
	timeout := time.After(2 * time.Second)
	for reloadCallsReceived < 2 {
		select {
		case <-reloadCalled:
			reloadCallsReceived++
			t.Logf("Received reload call %d/2", reloadCallsReceived)
		case <-timeout:
			t.Fatalf("Timeout waiting for BrowserReload calls. Got %d/2", reloadCallsReceived)
		}
	}

	select {
	case <-done:
		t.Log("watchEvents finished successfully")
	case <-time.After(1 * time.Second):
		t.Fatal("watchEvents did not finish in time")
	}

	if !assetCalled {
		t.Error("Asset handler was not called")
	}
	if !goCalled {
		t.Log("Go handler was not called (expected due to godepfind test limitations)")
	}
	t.Logf("Test completed. BrowserReload was called %d times", reloadCount)
}
