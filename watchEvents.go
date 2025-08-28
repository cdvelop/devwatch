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
	// 1) Extraer la extensión del archivo para determinar cómo procesarlo
	//    (archivos .go vs archivos de assets como .css, .js, .html)
	extension := filepath.Ext(eventName)
	var processError error

	// 2) Manejar archivos de assets (CSS, JS, HTML, imágenes, etc.)
	//    Estos archivos van directamente al handler de assets sin análisis de dependencias
	if slices.Contains(h.supportedAssetsExtensions, extension) {
		processError = h.FileEventAssets.NewFileEvent(fileName, extension, eventName, eventType)
		if processError != nil {
			if isDeleteEvent {
				fmt.Fprintln(h.Logger, "delete asset file error:", processError)
			}
		} else {
			// Trigger browser reload for asset files
			h.triggerBrowserReload()
		}
		return
	}

	// 3) Manejar archivos Go (.go) - requieren análisis de dependencias más complejo
	if extension == ".go" {
		// Log all Go file events (debug prints removed in production build)

		if isDeleteEvent {
			// 4) Para eventos de eliminación: permitir que todos los handlers procesen
			//    porque no podemos analizar dependencias de un archivo eliminado
			for _, handler := range h.FilesEventGO {
				_ = handler.NewFileEvent(fileName, extension, eventName, eventType)
			}
		} else {
			// 5) Para eventos que NO son eliminación: usar el dependency finder
			//    para determinar qué handler debería procesar este archivo específico
			fmt.Fprintf(h.Logger, "Processing non-delete event for Go file, handlers count=%d\n", len(h.FilesEventGO))
			for i, handler := range h.FilesEventGO {
				// 6) Obtener la ruta relativa del archivo main que maneja este handler
				//    (ej: "pwa/main.server.go", "cmd/main.go", etc.)
				fmt.Fprintf(h.Logger, "Checking handler %d: MainInputFileRelativePath=%s\n", i, handler.MainInputFileRelativePath())

				// 7) Preguntar al dependency finder: "¿Este archivo pertenece a este handler?"
				//    El finder analiza imports y dependencias para responder
				isMine, herr := h.depFinder.ThisFileIsMine(handler.MainInputFileRelativePath(), eventName, eventType)
				fmt.Fprintf(h.Logger, "ThisFileIsMine result: isMine=%v, err=%v\n", isMine, herr)

				if herr != nil {
					// 8) Si hay error en el análisis, continuar con el siguiente handler
					fmt.Fprintf(h.Logger, "Error from ThisFileIsMine, continuing: %v\n", herr)
					continue
				}

				if isMine {
					// 9) Si el archivo pertenece a este handler, procesarlo y parar el bucle
					//    Solo UN handler debe procesar cada archivo para evitar duplicación
					fmt.Fprintf(h.Logger, "Handler with MainInputFileRelativePath=%s claims this file, calling NewFileEvent\n", handler.MainInputFileRelativePath())
					processError = handler.NewFileEvent(fileName, extension, eventName, eventType)
					fmt.Fprintf(h.Logger, "NewFileEvent result: err=%v\n", processError)
					break
				} else {
					// 10) Si el archivo NO pertenece a este handler, continuar con el siguiente
					fmt.Fprintf(h.Logger, "Handler with MainInputFileRelativePath=%s does NOT claim this file\n", handler.MainInputFileRelativePath())
				}
			}
		}

		// 11) Disparar recarga del navegador para archivos Go (solo si no hubo errores)
		//     Esto notifica al navegador que debe refrescar la página
		if processError == nil {
			//fmt.Fprintln(h.Logger, "DEBUG: Triggering browser reload for Go file")
			h.triggerBrowserReload()
		} else {
			//fmt.Fprintf(h.Logger, "DEBUG: NOT triggering browser reload due to error: %v\n", processError)
		}
	}

	/* if processError != nil {
		fmt.Fprintln(h.Logger, "Watch updating file:", processError)
	} */
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
