package devwatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func (h *DevWatch) watchEvents() {
	// Restored but with shorter debounce time to avoid missing important events
	lastActions := make(map[string]time.Time)

	reloadBrowserTimer := time.NewTimer(0)
	reloadBrowserTimer.Stop()

	restarTimer := time.NewTimer(0)
	restarTimer.Stop()

	var wait = 50 * time.Millisecond

	for {
		select {

		case event, ok := <-h.watcher.Events:
			if !ok {
				fmt.Fprintln(h.Logger, "Error h.watcher.Events")
				return
			} // fmt.Fprintln(h.Logger, "DEBUG Event:", event.Name, event.Op)
			// Restore debouncer with shorter timeout - 100ms is enough for file operations to complete,
			// but short enough to not miss important events like CREATE followed by WRITE
			if lastTime, ok := lastActions[event.Name]; !ok || time.Since(lastTime) > 100*time.Millisecond {
				// Update the last action time for debouncing FIRST
				lastActions[event.Name] = time.Now()

				// Restablece el temporizador de recarga de navegador
				reloadBrowserTimer.Stop()

				// Verificar si es un nuevo directorio para agregarlo al watcher
				if info, err := os.Stat(event.Name); err == nil && !h.Contain(event.Name) {

					// create, write, rename, remove
					eventType := strings.ToLower(event.Op.String())
					// fmt.Fprintln(h.Writer, "Event type:", event.Op.String(), "File changed:", event.Name)

					// Get fileName once and reuse
					fileName, err := GetFileName(event.Name)
					if err == nil {
						// Handle directory changes for architecture detection
						if info.IsDir() {
							if h.FolderEvents != nil {
								err = h.FolderEvents.NewFolderEvent(fileName, event.Name, eventType)
								if err != nil {
									fmt.Fprintln(h.Logger, "Watch folder event error:", err)
								}
							}
							// Add new directory to watcher
							if eventType == "create" {
								// Create a registry map for the new directory walk
								reg := make(map[string]struct{})

								// Add the main directory first
								if err := h.addDirectoryToWatcher(event.Name, reg); err == nil {
									// Walk recursively to add any subdirectories that might have been created
									// This handles cases like os.MkdirAll() where multiple directories are created at once
									err := filepath.Walk(event.Name, func(path string, info os.FileInfo, err error) error {
										if err != nil {
											return nil // Continue walking even if there's an error
										}
										if info.IsDir() && path != event.Name && !h.Contain(path) {
											h.addDirectoryToWatcher(path, reg)
										}
										return nil
									})
									if err != nil {
										fmt.Fprintln(h.Logger, "Watch: Error walking new directory:", event.Name, err)
									}
								}
							}
						} else {
							// Handle file changes (existing logic)

							extension := filepath.Ext(event.Name)
							handled := false
							if slices.Contains(h.supportedAssetsExtensions, extension) {
								err = h.FileEventAssets.NewFileEvent(fileName, extension, event.Name, eventType)
								handled = true
							}
							if handled {
								// already handled as asset, skip to timer reset
								if err != nil {
									fmt.Fprintln(h.Logger, "Watch updating file:", err)
								} else {
									reloadBrowserTimer.Reset(wait)
								}
								continue
							}
							switch extension {
							case ".go":
								//handlerFound := false
								for _, handler := range h.FilesEventGO {
									isMine, herr := h.depFinder.ThisFileIsMine(handler, fileName, event.Name, eventType)
									if herr != nil {
										// Log error but continue to next handler
										//fmt.Fprintln(h.Writer, "Watch handler check error:", herr)
										// mostrar el error es irrelevante ya que puede que se creare un nuevo archivo vació
										continue
									}
									if isMine {
										err = handler.NewFileEvent(fileName, extension, event.Name, eventType)
										//handlerFound = true
									}
								}
							/* 	if !handlerFound { // no se requiere saber
								fmt.Fprintln(h.Writer, "No handler found for go file: "+fileName)
							} */

							default:
								err = errors.New("Watch Unknown file type: " + extension)
							}

							if err != nil {
								fmt.Fprintln(h.Logger, "Watch updating file:", err)
							} else {
								reloadBrowserTimer.Reset(wait)
							}
						}
					}
					// lastActions ya se actualiza al inicio del bloque if
				}
			}

		case err, ok := <-h.watcher.Errors:
			if !ok {
				fmt.Fprintln(h.Logger, "h.watcher.Errors:", err)
				return
			}

		case <-reloadBrowserTimer.C:
			// El temporizador de recarga ha expirado, ejecuta reload del navegador
			err := h.BrowserReload()
			if err != nil {
				fmt.Fprintln(h.Logger, "Watch:", err)
			}

		case <-h.ExitChan:
			h.watcher.Close()
			return
		}
	}
}
