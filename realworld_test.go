package devwatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// mockFileHandler simulates a FilesEventHandlers with specific unobserved files
type mockFileHandler struct {
	unobservedFiles []string
	eventsReceived  []string // Track which files were notified
}

func (m *mockFileHandler) MainInputFileRelativePath() string {
	return "main.go"
}

func (m *mockFileHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	m.eventsReceived = append(m.eventsReceived, filePath)
	return nil
}

func (m *mockFileHandler) SupportedExtensions() []string {
	return []string{".go", ".js", ".css"}
}

func (m *mockFileHandler) UnobservedFiles() []string {
	return m.unobservedFiles
}

func TestInitialRegistrationFiltersUnobservedFiles(t *testing.T) {
	// Create a temporary test directory structure
	tempDir := t.TempDir()

	// Create test files and directories
	testStructure := []string{
		"main.exe",   // Should be filtered by handler
		"output.log", // Should be filtered by handler
		"styles.css",
		"app.css",
		".git/config",           // Should be filtered by WatchConfig
		".vscode/settings.json", // Should be filtered by WatchConfig
		"src/app.js",
		"src/test.js", // Should be filtered by handler
		"src/main.js",
	}

	for _, file := range testStructure {
		fullPath := filepath.Join(tempDir, file)
		dir := filepath.Dir(fullPath)

		// Create directory if it doesn't exist
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		// Create file
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", fullPath, err)
		}
	}

	// Create mock handler
	mockHandler := &mockFileHandler{
		unobservedFiles: []string{
			".exe",
			".log",
			"test.js",
		},
		eventsReceived: []string{},
	}

	// Create DevWatch with both WatchConfig and handler unobserved files
	dw := &DevWatch{
		WatchConfig: &WatchConfig{
			AppRootDir:         tempDir,
			FilesEventHandlers: []FilesEventHandlers{mockHandler},
			UnobservedFiles: func() []string {
				return []string{
					".git",
					".vscode",
				}
			},
			Logger: func(message ...any) {
				// Silent logger for tests
			},
		},
	}

	// Initialize watcher (required for InitialRegistration)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()
	dw.watcher = watcher

	// Use New() to properly initialize DevWatch
	dw = New(dw.WatchConfig)
	dw.watcher = watcher

	// Run initial registration
	dw.InitialRegistration()

	// Verify that unobserved files were NOT notified
	shouldBeFiltered := []string{
		filepath.Join(tempDir, "main.exe"),
		filepath.Join(tempDir, "output.log"),
		filepath.Join(tempDir, "src/test.js"),
		filepath.Join(tempDir, ".git/config"),
		filepath.Join(tempDir, ".vscode/settings.json"),
	}

	for _, filteredPath := range shouldBeFiltered {
		for _, receivedPath := range mockHandler.eventsReceived {
			if receivedPath == filteredPath {
				t.Errorf("File should have been filtered but was notified: %s", filteredPath)
			}
		}
	}

	// Verify that valid files WERE notified
	shouldBeNotified := []string{
		filepath.Join(tempDir, "styles.css"),
		filepath.Join(tempDir, "app.css"),
		filepath.Join(tempDir, "src/app.js"),
		filepath.Join(tempDir, "src/main.js"),
	}

	for _, expectedPath := range shouldBeNotified {
		found := false
		for _, receivedPath := range mockHandler.eventsReceived {
			if receivedPath == expectedPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("File should have been notified but was filtered: %s", expectedPath)
		}
	}

	//t.Logf("Events received: %d", len(mockHandler.eventsReceived))
	//t.Logf("Files notified: %v", mockHandler.eventsReceived)
}

func TestContainWithMultipleHandlers(t *testing.T) {
	handler1 := &mockFileHandler{
		unobservedFiles: []string{".exe", ".log"},
	}

	handler2 := &mockFileHandler{
		unobservedFiles: []string{"test.js", "vendor"},
	}

	dw := &DevWatch{
		WatchConfig: &WatchConfig{
			AppRootDir:         "/test",
			FilesEventHandlers: []FilesEventHandlers{handler1, handler2},
			UnobservedFiles: func() []string {
				return []string{".git", ".vscode"}
			},
			Logger: func(message ...any) {},
		},
	}

	// Initialize the map by calling InitialRegistration or manually
	dw.noAddMu.Lock()
	if dw.no_add_to_watch == nil {
		dw.no_add_to_watch = make(map[string]bool)
	}

	if dw.UnobservedFiles != nil {
		for _, file := range dw.UnobservedFiles() {
			dw.no_add_to_watch[file] = true
		}
	}

	for _, handler := range dw.FilesEventHandlers {
		for _, file := range handler.UnobservedFiles() {
			dw.no_add_to_watch[file] = true
		}
	}
	dw.noAddMu.Unlock()

	tests := []struct {
		path     string
		expected bool
	}{
		{"/test/.git/config", true},           // From WatchConfig
		{"/test/.vscode/settings.json", true}, // From WatchConfig
		{"/test/main.exe", true},              // From handler1
		{"/test/output.log", true},            // From handler1
		{"/test/src/test.js", true},           // From handler2
		{"/test/vendor/package.json", true},   // From handler2
		{"/test/main.go", false},              // Should NOT be filtered
		{"/test/src/app.js", false},           // Should NOT be filtered
	}

	for _, tt := range tests {
		result := dw.Contain(tt.path)
		if result != tt.expected {
			t.Errorf("Contain(%s) = %v, expected %v", tt.path, result, tt.expected)
		}
	}
}
