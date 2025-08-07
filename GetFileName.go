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

	// Check if path ends with a separator
	if len(path) > 0 && (path[len(path)-1] == '/' || path[len(path)-1] == '\\') {
		return "", errors.New("GetFileName invalid path: ends with separator")
	}

	fileName := filepath.Base(path)
	if fileName == "." || fileName == string(filepath.Separator) {
		return "", errors.New("GetFileName invalid path")
	}
	if len(path) > 0 && path[len(path)-1] == filepath.Separator {
		return "", errors.New("GetFileName invalid path")
	}

	return fileName, nil
}
