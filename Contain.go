package devwatch

import (
	"path/filepath"
	"strings"
)

func (h *DevWatch) Contain(path string) bool {

	// Normaliza la ruta a formato Unix para compatibilidad multiplataforma
	// Convertir manualmente las barras invertidas a barras normales
	normPath := strings.ReplaceAll(path, "\\", "/")

	// ignore hidden files
	if strings.HasPrefix(filepath.Base(normPath), ".") {
		return true
	}

	if h.no_add_to_watch == nil {
		h.no_add_to_watch = map[string]bool{}

		// add files to ignore only if UnobservedFiles is configured
		if h.UnobservedFiles != nil {
			for _, file := range h.UnobservedFiles() {
				h.no_add_to_watch[file] = true
			}
		}
	}

	// Check for exact match against the full paths in the ignore list
	if _, exists := h.no_add_to_watch[normPath]; exists {
		return true
	}

	// Split the normalized path into components
	pathParts := strings.Split(normPath, "/")

	// Check each part of the path against ignored files/directories
	for _, part := range pathParts {
		if part == "" {
			continue
		}
		if _, exists := h.no_add_to_watch[part]; exists {
			return true
		}
	}

	// Additionally, check for paths that start with an ignored path + separator (usando '/' universalmente)
	for ignoredPath := range h.no_add_to_watch {
		ignoredNorm := filepath.ToSlash(ignoredPath)
		if strings.HasPrefix(normPath, ignoredNorm+"/") {
			return true
		}
	}

	return false
}
