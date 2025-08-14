package devwatch

import (
	"fmt"
	"sync"

	"github.com/fsnotify/fsnotify"
)

func (h *DevWatch) FileWatcherStart(wg *sync.WaitGroup) {

	if h.watcher == nil {
		if watcher, err := fsnotify.NewWatcher(); err != nil {
			fmt.Fprintln(h.Logger, "Error New Watcher: ", err)
			return
		} else {
			h.watcher = watcher
		}
	}

	// Start watching in the main routine
	go h.watchEvents()
	h.InitialRegistration()

	fmt.Fprintln(h.Logger, "Listening for File Changes ...")
	// Wait for exit signal after watching is active

	<-h.ExitChan
	h.watcher.Close()
	wg.Done()
}
