package devwatch

import (
	"io"
	"sync"

	"github.com/cdvelop/godepfind"
	"github.com/fsnotify/fsnotify"
)

// event: create, remove, write, rename
type FileEvent interface {
	NewFileEvent(fileName, extension, filePath, event string) error
}

type MainHandler interface {
	MainInputFileRelativePath() string // eg: "app/server/main.go"
}

type GoFileHandler interface {
	MainHandler
	FileEvent
}

// event: create, remove, write, rename
type FolderEvent interface {
	NewFolderEvent(folderName, path, event string) error
}

type WatchConfig struct {
	AppRootDir      string          // eg: "home/user/myNewApp"
	FileEventAssets FileEvent       // when change assets files eg: css, js, html, png, jpg, svg, etc event: create, remove, write, rename
	FilesEventGO    []GoFileHandler // handlers for go file events (backend, wasm, etc)
	FolderEvents    FolderEvent     // when directories are created/removed for architecture detection

	BrowserReload func() error // when change frontend files reload browser

	Logger          io.Writer       // For logging output
	ExitChan        chan bool       // global channel to signal the exit
	UnobservedFiles func() []string // files that are not observed by the watcher eg: ".git", ".gitignore", ".vscode",  "examples",
}

type DevWatch struct {
	*WatchConfig
	watcher                   *fsnotify.Watcher
	depFinder                 *godepfind.GoDepFind // Dependency finder for Go projects
	no_add_to_watch           map[string]bool
	noAddMu                   sync.RWMutex
	supportedAssetsExtensions []string
	// logMu           sync.Mutex // No longer needed with Print func
}

func New(c *WatchConfig) *DevWatch {
	dw := &DevWatch{
		WatchConfig:               c,
		depFinder:                 godepfind.New(c.AppRootDir),
		supportedAssetsExtensions: []string{".html", ".css", ".js", ".svg"},
	}
	return dw
}

// AddSupportedAssetsExtensions adds one or more file extensions to the supported assets list.
// By default, the following extensions are included: .html, .css, .js, .svg
// It ensures no duplicates are added.
// Example: dw.AddSupportedAssetsExtensions(".png", ".jpg")
func (dw *DevWatch) AddSupportedAssetsExtensions(exts ...string) {
	existing := make(map[string]struct{}, len(dw.supportedAssetsExtensions))
	for _, e := range dw.supportedAssetsExtensions {
		existing[e] = struct{}{}
	}
	for _, ext := range exts {
		if _, found := existing[ext]; !found {
			dw.supportedAssetsExtensions = append(dw.supportedAssetsExtensions, ext)
			existing[ext] = struct{}{}
		}
	}
}
