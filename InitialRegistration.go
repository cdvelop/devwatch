package devwatch

import (
	"os"
	"path/filepath"
	"slices"
)

// addDirectoryToWatcher adds a directory to the watcher and handles folder events
// This method is reused both in InitialRegistration and when new directories are created
func (h *DevWatch) addDirectoryToWatcher(path string, reg map[string]struct{}) error {
	if _, exists := reg[path]; exists {
		return nil // Already registered
	}

	if err := h.watcher.Add(path); err != nil {
		h.Logger("Failed to add directory to watcher:", path, err)
		return err
	}

	reg[path] = struct{}{}
	h.Logger("path added:", path)

	// Get fileName once and reuse
	fileName, err := GetFileName(path)
	if err == nil {
		// NOTIFY FOLDER EVENTS HANDLER FOR ARCHITECTURE DETECTION
		if h.FolderEvents != nil {
			err = h.FolderEvents.NewFolderEvent(fileName, path, "create")
			if err != nil {
				h.Logger("folder event error:", err)
			}
		}
	}

	if err != nil {
		h.Logger("addDirectoryToWatcher:", err)
	}

	return nil
}

func (h *DevWatch) InitialRegistration() {
	h.Logger("Registration APP ROOT DIR: " + h.AppRootDir)

	reg := make(map[string]struct{})

	err := filepath.Walk(h.AppRootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			h.Logger("accessing path:", path, err)
			return nil
		}

		if info.IsDir() && !h.Contain(path) {
			h.addDirectoryToWatcher(path, reg)
		} else if !info.IsDir() {
			// Process existing files during initial registration
			fileName, ferr := GetFileName(path)
			if ferr == nil {
				extension := filepath.Ext(path)

				for _, handler := range h.FilesEventHandlers {
					if slices.Contains(handler.SupportedExtensions(), extension) {
						var isMine = true
						var herr error

						if extension == ".go" {
							isMine, herr = h.depFinder.ThisFileIsMine(handler.MainInputFileRelativePath(), path, "create")
							if herr != nil {
								h.Logger("InitialRegistration go file error:", herr)
								continue // Skip on error
							}
						}

						if isMine {
							err = handler.NewFileEvent(fileName, extension, path, "create")
							if err != nil {
								h.Logger("InitialRegistration file error:", err)
							}
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		h.Logger("Walking directory:", err)
	}
}
