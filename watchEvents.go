package devwatch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (h *DevWatch) watchEvents() {
	// Track last event START time per file for debouncing
	// Use a shorter debounce (100ms) to allow rapid development iterations
	// while still filtering duplicate OS events
	lastEventStart := make(map[string]time.Time)
	const debounceWindow = 100 * time.Millisecond

	// create a stopped reload timer and a single goroutine that will handle its firing.
	h.reloadMutex.Lock()
	if h.reloadTimer == nil {
		h.reloadTimer = time.NewTimer(0)
		h.reloadTimer.Stop()
		// goroutine to wait on timer events and invoke reload
		go func(t *time.Timer) {
			for {
				<-t.C
				h.triggerBrowserReload()
			}
		}(h.reloadTimer)
	}
	h.reloadMutex.Unlock()

	for {
		select {

		case event, ok := <-h.watcher.Events:
			if !ok {
				h.Logger("Error h.watcher.Events")
				return
			}

			// Debounce: skip if we processed this file very recently (within 100ms)
			// This filters duplicate OS events while allowing rapid development iterations
			now := time.Now()
			if lastTime, exists := lastEventStart[event.Name]; exists && now.Sub(lastTime) <= debounceWindow {
				// Skip this event - we just processed this file (< 100ms ago)
				continue
			}

			// Record event start time BEFORE processing
			// This ensures the debounce window starts from when we BEGIN handling,
			// not when we finish (which could be much later for slow operations)
			lastEventStart[event.Name] = now

			// create, write, rename, remove
			eventType := strings.ToLower(event.Op.String())
			isDeleteEvent := eventType == "remove" || eventType == "delete"

			// For non-delete events, check if file exists and is not contained
			var info os.FileInfo
			if !isDeleteEvent {
				var statErr error
				info, statErr = os.Stat(event.Name)
				if statErr != nil || h.Contain(event.Name) {
					continue // Skip if file doesn't exist or is already contained
				}
			}

			// Get fileName once and reuse for all operations
			fileName, err := GetFileName(event.Name)
			if err != nil {
				continue // Skip if we can't get the filename
			}

			// Handle directory changes for architecture detection (only for non-delete events)
			if !isDeleteEvent && info.IsDir() {
				h.handleDirectoryEvent(fileName, event.Name, eventType)
				continue
			}

			// Handle file events (both delete and non-delete)
			h.handleFileEvent(fileName, event.Name, eventType, isDeleteEvent)

			// Note: We don't update lastEventStart here because we already did it
			// before processing (above). This ensures debounce is based on event
			// START time, not completion time.

		case err, ok := <-h.watcher.Errors:
			if !ok {
				h.Logger("h.watcher.Errors:", err)
				return
			}

		case <-h.ExitChan:
			h.watcher.Close()
			h.stopReload()
			return
		}
	}
}

// handleDirectoryEvent processes directory creation/modification events
func (h *DevWatch) handleDirectoryEvent(fileName, eventName, eventType string) {
	if h.FolderEvents != nil {
		err := h.FolderEvents.NewFolderEvent(fileName, eventName, eventType)
		if err != nil {
			h.Logger("Watch folder event error:", err)
		}
	}

	// Add new directory to watcher
	if eventType == "create" {
		// Create a registry map for the new directory walk
		reg := make(map[string]struct{})

		// Add the main directory first
		if err := h.addDirectoryToWatcher(eventName, reg); err == nil {
			// Walk recursively to add any subdirectories that might have been created
			// This handles cases like os.MkdirAll() where multiple directories are created at once
			err := filepath.Walk(eventName, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // Continue walking even if there's an error
				}
				if info.IsDir() && path != eventName && !h.Contain(path) {
					h.addDirectoryToWatcher(path, reg)
				}
				return nil
			})
			if err != nil {
				h.Logger("Watch: Error walking new directory:", eventName, err)
			}
		}
	}
}

// handleFileEvent processes file creation/modification/deletion events
func (h *DevWatch) handleFileEvent(fileName, eventName, eventType string, isDeleteEvent bool) {
	extension := filepath.Ext(eventName)
	var processedSuccessfully bool
	isGoFileEvent := extension == ".go"
	var goHandlerError error

	for _, handler := range h.FilesEventHandlers {
		if !slices.Contains(handler.SupportedExtensions(), extension) {
			continue
		}

		// At least one handler supports this extension.
		var isMine = true
		var herr error

		if !isDeleteEvent && extension == ".go" {
			isMine, herr = h.depFinder.ThisFileIsMine(handler.MainInputFileRelativePath(), eventName, eventType)
			if herr != nil {
				h.Logger("Error from ThisFileIsMine, continuing: %v\n", herr)
				continue
			}
		}

		if isMine {
			err := handler.NewFileEvent(fileName, extension, eventName, eventType)
			if err != nil {
				h.Logger("Watch updating file error:", err)
				if isGoFileEvent {
					goHandlerError = err
				}
			} else {
				processedSuccessfully = true
			}
		}
	}

	// For non-go files, schedule reload only if processed successfully.
	// For go files, schedule reload if no handler returned an error.
	if (isGoFileEvent && goHandlerError == nil) || (!isGoFileEvent && processedSuccessfully) {
		h.scheduleReload()
	}
}

// triggerBrowserReload safely triggers a browser reload in a goroutine
func (h *DevWatch) triggerBrowserReload() {
	if h.BrowserReload != nil {
		// Call synchronously so the caller (watchEvents) completes the
		// reload action before returning. This prevents background reload
		// goroutines from racing with test teardown and shared counters.
		_ = h.BrowserReload()
	}
}

// scheduleReload resets or starts a reload timer which will call triggerBrowserReload
// after a short debounce period. This mirrors the original implementation's
// behavior of resetting the timer on each new event so only the last one triggers reload.
func (h *DevWatch) scheduleReload() {
	const wait = 50 * time.Millisecond

	h.reloadMutex.Lock()
	defer h.reloadMutex.Unlock()

	if h.reloadTimer == nil {
		h.reloadTimer = time.NewTimer(wait)
		return
	}

	// Stop existing timer and reset
	if !h.reloadTimer.Stop() {
		select {
		case <-h.reloadTimer.C:
		default:
		}
	}
	h.reloadTimer.Reset(wait)
}

// stopReload stops and clears the reload timer; used during shutdown
func (h *DevWatch) stopReload() {
	h.reloadMutex.Lock()
	defer h.reloadMutex.Unlock()
	if h.reloadTimer != nil {
		// Only trigger reload if timer was actually programmed (not stopped)
		// Check if there's a pending reload by trying to stop the timer
		if !h.reloadTimer.Stop() {
			// Timer already fired or was never started, check channel
			select {
			case <-h.reloadTimer.C:
				// Timer fired but reload not yet called, trigger it now
				h.reloadMutex.Unlock() // Unlock before calling reload to avoid deadlock
				h.triggerBrowserReload()
				h.reloadMutex.Lock() // Re-lock before returning
			default:
				// Timer was stopped or never programmed, don't reload
			}
		}
		// If Stop() returned true, timer was active and is now stopped
		// Don't trigger reload in this case
	}
}
