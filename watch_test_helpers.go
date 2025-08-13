package devwatch

import (
	"os"
	"testing"

	"github.com/fsnotify/fsnotify"
)

type FakeFileEvent struct{ Called *bool }

func (f *FakeFileEvent) NewFileEvent(fileName, extension, filePath, event string) error {
	*f.Called = true
	return nil
}

type FakeGoFileHandler struct{ Called *bool }

func (f *FakeGoFileHandler) NewFileEvent(fileName, extension, filePath, event string) error {
	*f.Called = true
	return nil
}

func (f *FakeGoFileHandler) MainFilePath() string      { return "fake/main.go" }
func (f *FakeGoFileHandler) Name() string              { return "FakeGoHandler" }
func (f *FakeGoFileHandler) UnobservedFiles() []string { return []string{"fake_output.exe"} }

// Helper to create temp files and go.mod for tests
func CreateTestFiles(t *testing.T, tempDir string) (cssFile, goFile string) {
	cssFile = tempDir + "/file.css"
	goFile = tempDir + "/file.go"
	goModContent := "module testmodule\n\ngo 1.21\n"
	if err := os.WriteFile(tempDir+"/go.mod", []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cssFile, []byte("body {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goFile, []byte("package main\nfunc main(){}"), 0644); err != nil {
		t.Fatal(err)
	}
	return
}

// Helper to create a DevWatch instance for tests
func NewTestDevWatch(t *testing.T, tempDir string, assetCalled, goCalled *bool, reloadCount *int, reloadCalled chan struct{}) (*DevWatch, *fsnotify.Watcher) {
	config := &WatchConfig{
		AppRootDir:      tempDir,
		FileEventAssets: &FakeFileEvent{Called: assetCalled},
		FilesEventGO:    []GoFileHandler{&FakeGoFileHandler{Called: goCalled}},
		BrowserReload: func() error {
			*reloadCount++
			reloadCalled <- struct{}{}
			return nil
		},
		Writer:   os.Stdout,
		ExitChan: make(chan bool, 1),
	}
	w := New(config)
	w.supportedAssetsExtensions = []string{".css"}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.watcher = watcher
	return w, watcher
}
