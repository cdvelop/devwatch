package devwatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
				fmt.Fprintln(h.Writer, "Error h.watcher.Events")
				return
			} // fmt.Fprintln(h.Writer, "DEBUG Event:", event.Name, event.Op)
			// Restore debouncer with shorter timeout - 100ms is enough for file operations to complete,
			// but short enough to not miss important events like CREATE followed by WRITE
			if lastTime, ok := lastActions[event.Name]; !ok || time.Since(lastTime) > 100*time.Millisecond {
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
									fmt.Fprintln(h.Writer, "Watch folder event error:", err)
								}
							}
							// Add new directory to watcher
							if eventType == "create" {
								if err := h.watcher.Add(event.Name); err != nil {
									fmt.Fprintln(h.Writer, "Watch: Failed to add new directory to watcher:", event.Name, err)
								} else {
									fmt.Fprintln(h.Writer, "Watch: New directory added to watcher:", event.Name)
								}
							}
						} else {
							// Handle file changes (existing logic)
							extension := filepath.Ext(event.Name)
							// fmt.Println("extension:", extension, "File Event:", event)
							isFrontend, isBackend := h.GoFileIsType(fileName)

							switch extension {

							case ".css", ".js", ".html":
								err = h.FileEventAssets.NewFileEvent(fileName, extension, event.Name, eventType)

							case ".go":

								if isFrontend { // compilar a wasm y recargar el navegador
									// fmt.Fprintln(h.Writer, "Go File IsFrontend")
									err = h.FileEventWASM.NewFileEvent(fileName, extension, event.Name, eventType)
								} else if isBackend { // compilar servidor y recargar el navegador
									// fmt.Fprintln(h.Writer, "Go File IsBackend")
									err = h.FileEventGO.NewFileEvent(fileName, extension, event.Name, eventType)

								} else { // ambos compilar servidor, compilar a wasm (según modulo) y recargar el navegador
									// fmt.Fprintln(h.Writer, "Go File Shared")
									err = h.FileEventWASM.NewFileEvent(fileName, extension, event.Name, eventType)
									if err == nil {
										err = h.FileEventGO.NewFileEvent(fileName, extension, event.Name, eventType)
									}
								}

							default:
								err = errors.New("Watch Unknown file type: " + extension)
							}

							if err != nil {
								fmt.Fprintln(h.Writer, "Watch updating file:", err)
							} else {
								reloadBrowserTimer.Reset(wait)
							}
						}
					}
					// Update the last action time for debouncing
					lastActions[event.Name] = time.Now()
				}
			}

		case err, ok := <-h.watcher.Errors:
			if !ok {
				fmt.Fprintln(h.Writer, "h.watcher.Errors:", err)
				return
			}

		case <-reloadBrowserTimer.C:
			// El temporizador de recarga ha expirado, ejecuta reload del navegador
			err := h.BrowserReload()
			if err != nil {
				fmt.Fprintln(h.Writer, "Watch:", err)
			}

		case <-h.ExitChan:
			h.watcher.Close()
			return
		}
	}
}
