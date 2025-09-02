package devwatch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (h *DevWatch) watchEvents() {
	// Use per-file debouncing like the original working implementation
	lastActions := make(map[string]time.Time)

	for {
		select {

		case event, ok := <-h.watcher.Events:
			if !ok {
				fmt.Fprintln(h.Logger, "Error h.watcher.Events")
				return
			}

			// Per-file debounce logic from original working implementation
			if lastTime, exists := lastActions[event.Name]; exists && time.Since(lastTime) <= 100*time.Millisecond {
				// Skip this event - it's within 100ms of the last event for this file
				continue
			}

			// Register this action for debouncing
			lastActions[event.Name] = time.Now()

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

		case err, ok := <-h.watcher.Errors:
			if !ok {
				fmt.Fprintln(h.Logger, "h.watcher.Errors:", err)
				return
			}

		case <-h.ExitChan:
			h.watcher.Close()
			return
		}
	}
}

// handleDirectoryEvent processes directory creation/modification events
func (h *DevWatch) handleDirectoryEvent(fileName, eventName, eventType string) {
	if h.FolderEvents != nil {
		err := h.FolderEvents.NewFolderEvent(fileName, eventName, eventType)
		if err != nil {
			fmt.Fprintln(h.Logger, "Watch folder event error:", err)
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
				fmt.Fprintln(h.Logger, "Watch: Error walking new directory:", eventName, err)
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
				fmt.Fprintf(h.Logger, "Error from ThisFileIsMine, continuing: %v\n", herr)
				continue
			}
		}

		if isMine {
			err := handler.NewFileEvent(fileName, extension, eventName, eventType)
			if err != nil {
				fmt.Fprintln(h.Logger, "Watch updating file error:", err)
				if isGoFileEvent {
					goHandlerError = err
				}
			} else {
				processedSuccessfully = true
			}
		}
	}

	// For non-go files, reload only if processed successfully.
	// For go files, reload if no handler returned an error.
	if (isGoFileEvent && goHandlerError == nil) || (!isGoFileEvent && processedSuccessfully) {
		h.triggerBrowserReload()
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
