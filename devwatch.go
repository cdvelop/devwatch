package devwatch

import (
	"io"

	"github.com/fsnotify/fsnotify"
)

// event: create, remove, write, rename
type FileEvent interface {
	NewFileEvent(fileName, extension, filePath, event string) error
}

// event: create, remove, write, rename
type FolderEvent interface {
	NewFolderEvent(folderName, path, event string) error
}

type WatchConfig struct {
	AppRootDir      string      // eg: "home/user/myNewApp"
	FileEventAssets FileEvent   // when change assets files eg: css, js, html, png, jpg, svg, etc event: create, remove, write, rename
	FileEventGO     FileEvent   // when change go files to backend or any destination
	FileEventWASM   FileEvent   // when change go files to webAssembly destination
	FolderEvents    FolderEvent // when directories are created/removed for architecture detection

	BrowserReload func() error // when change frontend files reload browser

	Writer          io.Writer       // For logging output
	ExitChan        chan bool       // global channel to signal the exit
	UnobservedFiles func() []string // files that are not observed by the watcher eg: ".git", ".gitignore", ".vscode",  "examples",
}

type DevWatch struct {
	*WatchConfig
	watcher         *fsnotify.Watcher
	no_add_to_watch map[string]bool
	// logMu           sync.Mutex // No longer needed with Print func
}

func New(c *WatchConfig) *DevWatch {

	return &DevWatch{
		WatchConfig: c,
	}
}
