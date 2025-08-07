package devwatch

import (
	"path/filepath"
	"strings"
)

func (h *DevWatch) Contain(path string) bool {

	// ignore hidden files
	if strings.HasPrefix(filepath.Base(path), ".") {
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
	if _, exists := h.no_add_to_watch[path]; exists {
		return true
	}

	// Split the path into components
	pathParts := strings.SplitSeq(filepath.ToSlash(path), "/")

	// Check each part of the path against ignored files/directories
	for part := range pathParts {
		if part == "" {
			continue
		}

		if _, exists := h.no_add_to_watch[part]; exists {
			return true
		}
	}

	// Additionally, check for paths that start with an ignored path + separator
	for ignoredPath := range h.no_add_to_watch {
		// Check if the current path starts with an ignored path + separator
		// This prevents watching subdirectories of ignored directories (like .git/hooks)
		if strings.HasPrefix(path, ignoredPath+string(filepath.Separator)) {
			return true
		}
	}

	return false
}
