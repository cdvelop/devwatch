package devwatch

import (
	"fmt"
	"os"
	"path/filepath"
)

func (h *DevWatch) InitialRegistration() {
	fmt.Fprintln(h.Writer, "InitialRegistration APP ROOT DIR: "+h.AppRootDir)

	reg := make(map[string]struct{})

	err := filepath.Walk(h.AppRootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintln(h.Writer, "accessing path:", path, err)
			return nil
		}
		if info.IsDir() && !h.Contain(path) {
			if _, exists := reg[path]; !exists {
				if err := h.watcher.Add(path); err != nil {
					fmt.Fprintln(h.Writer, "Watch InitialRegistration Add watch path:", path, err)
					return nil
				}
				reg[path] = struct{}{}
				fmt.Fprintln(h.Writer, "Watch path added:", path)

				// Get fileName once and reuse
				fileName, err := GetFileName(path)
				if err == nil { // NOTIFY FOLDER EVENTS HANDLER FOR ARCHITECTURE DETECTION
					if h.FolderEvents != nil {
						err = h.FolderEvents.NewFolderEvent(fileName, path, "create")
						if err != nil {
							fmt.Fprintln(h.Writer, "Watch InitialRegistration FolderEvents error:", err)
						}
					} // MEMORY REGISTER FILES IN HANDLERS
					extension := filepath.Ext(path)
					switch extension {
					case ".html", ".css", ".js", ".svg":
						err = h.FileEventAssets.NewFileEvent(fileName, extension, path, "create")
					}
				}

				if err != nil {
					fmt.Fprintln(h.Writer, "Watch InitialRegistration:", err)
				}

			}
		}
		return nil
	})

	if err != nil {
		fmt.Fprintln(h.Writer, "Walking directory:", err)
	}
}
