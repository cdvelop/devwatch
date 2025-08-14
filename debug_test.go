package devwatch

import (
	"fmt"
	"testing"
)

func TestDebugContain(t *testing.T) {
	// Simular el setup exacto que usa godev
	handler := &DevWatch{
		WatchConfig: &WatchConfig{
			UnobservedFiles: func() []string {
				fmt.Println("UnobservedFiles() called")
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

	// Test con una ruta que NO empiece con . para forzar la inicialización
	testPath := "some/normal/path"
	fmt.Printf("Testing path that doesn't start with dot: %s\n", testPath)
	result := handler.Contain(testPath)
	fmt.Printf("Result: %t\n", result)
	fmt.Printf("no_add_to_watch after init: %+v\n", handler.no_add_to_watch)

	// Ahora test las rutas problemáticas
	testPaths := []string{
		"/home/cesar/Dev/Pkg/Mine/godev/test/manual/.git/objects/dc",
		"manual/.git/objects",
	}

	for i, path := range testPaths {
		result := handler.Contain(path)
		fmt.Printf("%d. Path: %s -> Contain: %t (should be true to ignore)\n", i+1, path, result)
	}
}
