package devwatch

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// Test que usa el watcher real con archivos reales para detectar duplicación
func TestWatchEvents_RealFileDuplicateBug(t *testing.T) {
	tempDir := t.TempDir()

	// Crear archivos de test
	_, _ = CreateTestFiles(t, tempDir)
	htmlFile := tempDir + "/index.html"
	if err := os.WriteFile(htmlFile, []byte("<!DOCTYPE html><html></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Contadores para detectar duplicación
	assetCallCount := 0
	assetCalls := []string{}
	var reloadMu sync.Mutex
	reloadCount := 0
	reloadCalled := make(chan struct{}, 10) // Buffer grande para capturar duplicados

	// Crear contador thread-safe
	countingEvent := &CountingFileEvent{
		CallCount: &assetCallCount,
		Calls:     &assetCalls,
	}

	// Crear configuración personalizada para el test
	countingEvent.SupportedExtensions_ = []string{".html", ".css", ".js"}
	config := &WatchConfig{
		AppRootDir:         tempDir,
		FilesEventHandlers: []FilesEventHandlers{countingEvent},
		BrowserReload: func() error {
			reloadMu.Lock()
			reloadCount++
			reloadMu.Unlock()
			reloadCalled <- struct{}{}
			return nil
		},
		Logger:   func(message ...any) { fmt.Println(message...) },
		ExitChan: make(chan bool, 1),
	}

	w := New(config)

	var wg sync.WaitGroup
	wg.Add(1)

	// Iniciar el watcher real
	go w.FileWatcherStart(&wg)

	// Esperar a que el watcher esté listo y complete InitialRegistration
	time.Sleep(200 * time.Millisecond)

	// Reset counters after InitialRegistration to test only the debouncing behavior
	t.Log("Resetting counters after InitialRegistration")
	countingEvent.Reset()
	reloadMu.Lock()
	reloadCount = 0
	reloadMu.Unlock()

	// Escribir al archivo REAL con eventos muy rápidos para probar debouncing
	t.Log("Writing to real HTML file - rapid events")
	if err := os.WriteFile(htmlFile, []byte("<!DOCTYPE html><html><body>Modified 1</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Escribir inmediatamente (debería ser filtrado por debouncing)
	time.Sleep(50 * time.Millisecond) // Menos que el debounce de 100ms
	if err := os.WriteFile(htmlFile, []byte("<!DOCTYPE html><html><body>Modified 2</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Esperar procesamiento del primer batch
	time.Sleep(200 * time.Millisecond)

	// Escribir otra vez después del debounce
	t.Log("Writing to HTML file again after debounce period")
	if err := os.WriteFile(htmlFile, []byte("<!DOCTYPE html><html><body>Modified 3</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Esperar procesamiento
	time.Sleep(300 * time.Millisecond)

	// Cerrar el watcher
	w.ExitChan <- true
	wg.Wait()

	// Analizar resultados de manera thread-safe
	finalCallCount, finalCalls := countingEvent.GetCounts()
	reloadMu.Lock()
	finalReloadCount := reloadCount
	reloadMu.Unlock()

	t.Logf("Asset handler was called %d times", finalCallCount)
	t.Logf("Asset calls details: %v", finalCalls)
	t.Logf("Browser reload was called %d times", finalReloadCount)

	// Verificar duplicación
	t.Logf("Expected: 2 asset calls (one for rapid events batch, one for delayed event)")
	if finalCallCount == 2 {
		t.Log("✓ Asset handler called exactly twice - debouncing working correctly")
	} else if finalCallCount > 2 {
		t.Errorf("BUG DETECTED: Asset handler was called %d times, expected 2. Debouncing not working properly!", finalCallCount)
		t.Errorf("Duplicate calls were: %v", finalCalls)
	} else {
		t.Errorf("Asset handler was called only %d times, expected 2. Some events may be missing!", finalCallCount)
	}
}

// Test con múltiples tipos de archivos usando el watcher real
func TestWatchEvents_RealMultipleFiles_DuplicateBug(t *testing.T) {
	tempDir := t.TempDir()

	// Crear estructura de archivos
	cssFile, _ := CreateTestFiles(t, tempDir)
	htmlFile := tempDir + "/index.html"
	jsFile := tempDir + "/script.js"

	if err := os.WriteFile(htmlFile, []byte("<!DOCTYPE html><html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsFile, []byte("console.log('test');"), 0644); err != nil {
		t.Fatal(err)
	}

	assetCallCount := 0
	assetCalls := []string{}
	var reloadMu sync.Mutex
	reloadCount := 0
	reloadCalled := make(chan struct{}, 10)

	w, _, countingEvent := NewTestDevWatchForDuplication(t, tempDir, &assetCallCount, &assetCalls)

	// Override the browser reload function to track reload calls
	w.BrowserReload = func() error {
		reloadMu.Lock()
		reloadCount++
		reloadMu.Unlock()
		reloadCalled <- struct{}{}
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Start the watcher
	go w.FileWatcherStart(&wg)

	time.Sleep(100 * time.Millisecond)

	// Reset counters after InitialRegistration to test only the file modification behavior
	t.Log("Resetting counters after InitialRegistration")
	countingEvent.Reset()
	reloadMu.Lock()
	reloadCount = 0
	reloadMu.Unlock()

	// Escribir a múltiples archivos
	files := []string{htmlFile, cssFile, jsFile}
	contents := []string{
		"<!DOCTYPE html><html><body>Updated</body></html>",
		"body { color: red; }",
		"console.log('updated');",
	}

	for i, file := range files {
		t.Logf("Writing to %s", file)
		if err := os.WriteFile(file, []byte(contents[i]), 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond) // Separar los eventos
	}

	time.Sleep(500 * time.Millisecond) // Esperar procesamiento final

	w.ExitChan <- true
	wg.Wait()

	// Analizar resultados de manera thread-safe
	finalCallCount, finalCalls := countingEvent.GetCounts()
	t.Logf("Total asset handler calls: %d (expected: 3)", finalCallCount)
	t.Logf("Asset calls details: %v", finalCalls)

	expectedCalls := 3
	if finalCallCount == expectedCalls {
		t.Log("✓ Asset handler called correct number of times")
	} else if finalCallCount > expectedCalls {
		t.Errorf("BUG DETECTED: Asset handler called %d times, expected %d. Possible duplicate events!", finalCallCount, expectedCalls)
		t.Errorf("All calls: %v", finalCalls)
	} else {
		t.Errorf("Asset handler called %d times, expected %d. Some events may be missing!", finalCallCount, expectedCalls)
	}
}
