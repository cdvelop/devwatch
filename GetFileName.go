package devwatch

import (
	"errors"
	"path/filepath"
)

// GetFileName returns the filename from a path
// Example: "theme/index.html" -> "index.html"
func GetFileName(path string) (string, error) {
	if path == "" {
		return "", errors.New("GetFileName empty path")
	}

	// Check if path ends with a separator (either / or \)
	if len(path) > 0 && (path[len(path)-1] == '/' || path[len(path)-1] == '\\') {
		return "", errors.New("GetFileName invalid path: ends with separator")
	}

	// Normalize backslashes to slashes for cross-platform compatibility
	normPath := path
	for i := 0; i < len(normPath); i++ {
		if normPath[i] == '\\' {
			// Replace all backslashes with slashes
			normPath = ""
			for j := 0; j < len(path); j++ {
				if path[j] == '\\' {
					normPath += "/"
				} else {
					normPath += string(path[j])
				}
			}
			break
		}
	}

	fileName := filepath.Base(normPath)
	if fileName == "." || fileName == string(filepath.Separator) {
		return "", errors.New("GetFileName invalid path")
	}
	if len(normPath) > 0 && normPath[len(normPath)-1] == filepath.Separator {
		return "", errors.New("GetFileName invalid path")
	}

	return fileName, nil
}
