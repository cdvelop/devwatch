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
	var processError error

	// Handle asset files
	if slices.Contains(h.supportedAssetsExtensions, extension) {
		processError = h.FileEventAssets.NewFileEvent(fileName, extension, eventName, eventType)
		if processError != nil {
			if isDeleteEvent {
				fmt.Fprintln(h.Logger, "Watch delete asset file error:", processError)
			}
		} else {
			// Trigger browser reload for asset files
			h.triggerBrowserReload()
		}
		return
	}

	// Handle Go files
	if extension == ".go" {
		if isDeleteEvent {
			// For delete events, let all handlers try to process
			for _, handler := range h.FilesEventGO {
				_ = handler.NewFileEvent(fileName, extension, eventName, eventType)
			}
		} else {
			// For non-delete events, use dependency finder
			for _, handler := range h.FilesEventGO {
				isMine, herr := h.depFinder.ThisFileIsMine(handler, fileName, eventName, eventType)
				if herr != nil {
					continue
				}
				if isMine {
					processError = handler.NewFileEvent(fileName, extension, eventName, eventType)
					break
				}
			}
		}

		// Trigger browser reload for Go files (if no error occurred)
		if processError == nil {
			h.triggerBrowserReload()
		}
	}

	if processError != nil {
		fmt.Fprintln(h.Logger, "Watch updating file:", processError)
	}
}

// triggerBrowserReload safely triggers a browser reload in a goroutine
func (h *DevWatch) triggerBrowserReload() {
	go func() {
		if h.BrowserReload != nil {
			h.BrowserReload()
		}
	}()
}
