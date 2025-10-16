package devwatch

import (
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestAddHandlers(t *testing.T) {
	// Create initial DevWatch with one handler
	handler1 := &mockFileHandler{
		unobservedFiles: []string{".exe", ".log"},
		eventsReceived:  []string{},
	}

	dw := New(&WatchConfig{
		AppRootDir:         "/test",
		FilesEventHandlers: []FilesEventHandlers{handler1},
		UnobservedFiles: func() []string {
			return []string{".git", ".vscode"}
		},
		Logger: func(message ...any) {
			t.Log(message...)
		},
	})

	// Initialize watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()
	dw.watcher = watcher

	// Run initial registration to initialize the map
	dw.InitialRegistration()

	// Verify initial state
	if len(dw.FilesEventHandlers) != 1 {
		t.Errorf("Expected 1 initial handler, got %d", len(dw.FilesEventHandlers))
	}

	// Check that initial unobserved files are loaded
	if !dw.Contain("/test/main.exe") {
		t.Error("Expected .exe files to be filtered")
	}

	// Create additional handlers (simulating deploy section initialization)
	handler2 := &mockFileHandler{
		unobservedFiles: []string{"_worker.js", "app.wasm"},
		eventsReceived:  []string{},
	}

	handler3 := &mockFileHandler{
		unobservedFiles: []string{"dist", "node_modules"},
		eventsReceived:  []string{},
	}

	// Add handlers dynamically
	dw.AddFilesEventHandlers(handler2, handler3)

	// Verify handlers were added
	if len(dw.FilesEventHandlers) != 3 {
		t.Errorf("Expected 3 handlers after AddHandlers, got %d", len(dw.FilesEventHandlers))
	}

	// Verify that unobserved files from new handlers are now filtered
	tests := []struct {
		path     string
		expected bool
		reason   string
	}{
		{"/test/main.exe", true, "from handler1"},
		{"/test/output.log", true, "from handler1"},
		{"/test/.git/config", true, "from WatchConfig"},
		{"/test/.vscode/settings.json", true, "from WatchConfig"},
		{"/test/deploy/_worker.js", true, "from handler2"},
		{"/test/deploy/app.wasm", true, "from handler2"},
		{"/test/dist/bundle.js", true, "from handler3"},
		{"/test/node_modules/package.json", true, "from handler3"},
		{"/test/main.go", false, "should NOT be filtered"},
		{"/test/src/app.js", false, "should NOT be filtered"},
	}

	for _, tt := range tests {
		result := dw.Contain(tt.path)
		if result != tt.expected {
			t.Errorf("Contain(%s) = %v, expected %v (%s)", tt.path, result, tt.expected, tt.reason)
		}
	}
}

func TestAddHandlersBeforeInitialRegistration(t *testing.T) {
	// Test adding handlers before InitialRegistration is called
	dw := New(&WatchConfig{
		AppRootDir:         "/test",
		FilesEventHandlers: []FilesEventHandlers{},
		Logger: func(message ...any) {
			t.Log(message...)
		},
	})

	handler1 := &mockFileHandler{
		unobservedFiles: []string{".exe", ".log"},
	}

	handler2 := &mockFileHandler{
		unobservedFiles: []string{"_worker.js"},
	}

	// Add handlers before InitialRegistration
	dw.AddFilesEventHandlers(handler1, handler2)

	// Verify handlers were added
	if len(dw.FilesEventHandlers) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(dw.FilesEventHandlers))
	}

	// Verify that unobserved files are in the map
	dw.noAddMu.RLock()
	if !dw.no_add_to_watch[".exe"] {
		t.Error("Expected .exe to be in no_add_to_watch map")
	}
	if !dw.no_add_to_watch["_worker.js"] {
		t.Error("Expected _worker.js to be in no_add_to_watch map")
	}
	dw.noAddMu.RUnlock()
}
