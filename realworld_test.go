package devwatch

import (
	"fmt"
	"testing"
)

func TestRealWorldScenario(t *testing.T) {
	// Simular exactamente la configuración que usa godev
	handler := &DevWatch{
		WatchConfig: &WatchConfig{
			AppRootDir: "/home/cesar/Dev/Pkg/Mine/godev/test",
			UnobservedFiles: func() []string {
				return []string{
					".git",
					".gitignore",
					".vscode",
					".exe",
					".log",
					"_test.go",
				}
			},
		},
	}

	// Estas son las rutas que están apareciendo en los logs
	problematicPaths := []string{
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git",
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/objects",
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/objects/dc",
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/logs",
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/logs/refs",
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/logs/refs/heads",
	}

	fmt.Println("Testing problematic paths that should be filtered out:")
	allFiltered := true

	for _, path := range problematicPaths {
		shouldIgnore := handler.Contain(path)
		status := "✓ FILTERED"
		if !shouldIgnore {
			status = "✗ NOT FILTERED (PROBLEM!)"
			allFiltered = false
		}
		fmt.Printf("Path: %s -> %s\n", path, status)
	}

	if !allFiltered {
		t.Error("Some .git paths are not being filtered correctly")
	}
}
