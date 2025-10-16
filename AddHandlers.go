package devwatch

// AddHandlers allows adding handlers dynamically after DevWatch initialization.
// This is useful when handlers are created after the watcher starts (e.g., deploy handlers).
// The method extracts UnobservedFiles from each handler and adds them to the no_add_to_watch map.
func (h *DevWatch) AddFilesEventHandlers(handlers ...FilesEventHandlers) {
	h.noAddMu.Lock()
	defer h.noAddMu.Unlock()

	// Initialize map if needed
	if h.no_add_to_watch == nil {
		h.no_add_to_watch = make(map[string]bool)
	}

	// Add each handler to FilesEventHandlers list
	h.FilesEventHandlers = append(h.FilesEventHandlers, handlers...)

	// Load unobserved files from each new handler
	for _, handler := range handlers {
		for _, file := range handler.UnobservedFiles() {
			h.no_add_to_watch[file] = true
		}
	}

	//h.Logger("Added", len(handlers), "handler(s) with unobserved files to watcher")
}
