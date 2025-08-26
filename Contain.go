package devwatch

import (
	"path/filepath"
	"strings"
)

func (h *DevWatch) Contain(path string) bool {

	// Normaliza la ruta a formato Unix para compatibilidad multiplataforma
	// Convertir manualmente las barras invertidas a barras normales
	normPath := strings.ReplaceAll(path, "\\", "/")

	// Initialize the no_add_to_watch map if needed, BEFORE any checks
	// Use a mutex to avoid concurrent map read/write races when tests or
	// different goroutines call Contain concurrently while the map is being
	// initialized or populated.
	// Note: we prefer to take a write lock only when initialization is
	// necessary; otherwise use a read lock for lookups.
	h.noAddMu.Lock()
	if h.no_add_to_watch == nil {
		h.no_add_to_watch = map[string]bool{}

		// add files to ignore only if UnobservedFiles is configured
		if h.UnobservedFiles != nil {
			unobservedList := h.UnobservedFiles()
			for _, file := range unobservedList {
				h.no_add_to_watch[file] = true
			}
		}
	}
	h.noAddMu.Unlock()

	// Check for exact match against the full paths in the ignore list FIRST
	h.noAddMu.RLock()
	if _, exists := h.no_add_to_watch[normPath]; exists {
		h.noAddMu.RUnlock()
		/* if strings.Contains(normPath, ".git") && h.Writer != nil {
			fmt.Fprintf(h.Writer, "[DEBUG] Exact match found for %s - RETURNING TRUE\n", normPath)
		} */
		return true
	}

	// Split the normalized path into components and check each part
	pathParts := strings.Split(normPath, "/")
	for _, part := range pathParts {
		if part == "" {
			continue
		}
		if _, exists := h.no_add_to_watch[part]; exists {
			/* 	if strings.Contains(normPath, ".git") && h.Writer != nil {
				fmt.Fprintf(h.Writer, "[DEBUG] Part match found: %s in %s - RETURNING TRUE\n", part, normPath)
			} */
			h.noAddMu.RUnlock()
			return true
		}
	}

	// Additionally, check for paths that start with an ignored path + separator
	for ignoredPath := range h.no_add_to_watch {
		ignoredNorm := filepath.ToSlash(ignoredPath)
		if strings.HasPrefix(normPath, ignoredNorm+"/") {
			/* if strings.Contains(normPath, ".git") && h.Writer != nil {
				fmt.Fprintf(h.Writer, "[DEBUG] Prefix match found: %s starts with %s/ - RETURNING TRUE\n", normPath, ignoredNorm)
			} */
			h.noAddMu.RUnlock()
			return true
		}
	}
	h.noAddMu.RUnlock()

	// ignore other hidden files (but not .git which is handled above)
	baseName := filepath.Base(normPath)
	if strings.HasPrefix(baseName, ".") && baseName != ".git" {
		/* if strings.Contains(normPath, ".git") && h.Writer != nil {
			fmt.Fprintf(h.Writer, "[DEBUG] Hidden file (not .git): %s - RETURNING TRUE\n", normPath)
		} */
		return true
	}

	/* if strings.Contains(normPath, ".git") && h.Writer != nil {
		fmt.Fprintf(h.Writer, "[DEBUG] No match found for %s - RETURNING FALSE (THIS IS THE PROBLEM!)\n", normPath)
	} */

	return false
}
