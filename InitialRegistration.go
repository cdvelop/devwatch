package devwatch

import (
	"fmt"
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
		fmt.Fprintln(h.Logger, "Failed to add directory to watcher:", path, err)
		return err
	}

	reg[path] = struct{}{}
	fmt.Fprintln(h.Logger, "path added:", path)

	// Get fileName once and reuse
	fileName, err := GetFileName(path)
	if err == nil {
		// NOTIFY FOLDER EVENTS HANDLER FOR ARCHITECTURE DETECTION
		if h.FolderEvents != nil {
			err = h.FolderEvents.NewFolderEvent(fileName, path, "create")
			if err != nil {
				fmt.Fprintln(h.Logger, "folder event error:", err)
			}
		}
		// MEMORY REGISTER FILES IN HANDLERS
		extension := filepath.Ext(path)
		if slices.Contains(h.supportedAssetsExtensions, extension) {
			err = h.FileEventAssets.NewFileEvent(fileName, extension, path, "create")
		}
	}

	if err != nil {
		fmt.Fprintln(h.Logger, "addDirectoryToWatcher:", err)
	}

	return nil
}

func (h *DevWatch) InitialRegistration() {
	fmt.Fprintln(h.Logger, "Registration APP ROOT DIR: "+h.AppRootDir)

	reg := make(map[string]struct{})

	err := filepath.Walk(h.AppRootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintln(h.Logger, "accessing path:", path, err)
			return nil
		}

		if info.IsDir() && !h.Contain(path) {
			h.addDirectoryToWatcher(path, reg)
		} else if !info.IsDir() {
			// Process existing files during initial registration
			fileName, ferr := GetFileName(path)
			if ferr == nil {
				extension := filepath.Ext(path)

				// Handle asset files (CSS, JS, SVG, HTML)
				if slices.Contains(h.supportedAssetsExtensions, extension) {
					//fmt.Fprintln(h.Logger, "InitialRegistration processing asset file:", path)
					err = h.FileEventAssets.NewFileEvent(fileName, extension, path, "create")
					if err != nil {
						fmt.Fprintln(h.Logger, "InitialRegistration asset file error:", err)
					}
				}

				// Handle Go files
				if extension == ".go" {
					for _, handler := range h.FilesEventGO {
						isMine, herr := h.depFinder.ThisFileIsMine(handler, fileName, path, "create")
						if herr != nil {
							continue // Skip errors during initial registration
						}
						if isMine {
							//fmt.Fprintln(h.Logger, "InitialRegistration processing go file:", path, "for handler:", handler.Name())
							err = handler.NewFileEvent(fileName, extension, path, "create")
							if err != nil {
								fmt.Fprintln(h.Logger, "InitialRegistration go file error:", err)
							}
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		fmt.Fprintln(h.Logger, "Walking directory:", err)
	}
}
