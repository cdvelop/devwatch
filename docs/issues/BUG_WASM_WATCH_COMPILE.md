# BUG: Browser Reloads Without WASM Compilation

## Status
🔴 **CRITICAL** - Browser reloads before WASM compilation completes

## Affected Version
- golite: current
- tinywasm: current
- devwatch: current
- godepfind: v0.0.15

---

## Problem Statement

When editing the WASM main file (`src/cmd/webclient/main.go`) in the example project, the browser reloads immediately without waiting for WASM recompilation to complete. This results in the browser loading stale WASM code.

### Expected Behavior
1. User edits `src/cmd/webclient/main.go`
2. File watcher detects change
3. WASM compilation starts and completes
4. Browser reloads with new WASM

### Actual Behavior
1. User edits `src/cmd/webclient/main.go`
2. File watcher detects change
3. Browser reloads immediately
4. WASM compilation may or may not have started/completed

---

## Reproduction Steps

1. Navigate to `golite/example` directory
2. Start golite with `go run ../cmd/golite/main.go`
3. Open browser to `https://localhost:4430`
4. Edit `src/cmd/webclient/main.go` (e.g., change "Hello, WebAssembly! 0" to "Hello, WebAssembly! 1")
5. Save the file
6. Observe: Browser reloads but shows old content

**Workaround:** Manually refresh browser after a few seconds

---

## Root Cause Analysis

### Investigation Summary

The issue is NOT in `godepfind.ThisFileIsMine()` - this function correctly identifies that `src/cmd/webclient/main.go` belongs to the WASM handler.

The problem lies in the **reload timing** in `devwatch/watchEvents.go`:

#### Current Flow (INCORRECT)

```go
// devwatch/watchEvents.go - handleFileEvent()

for _, handler := range h.FilesEventHandlers {
    if !slices.Contains(handler.SupportedExtensions(), extension) {
        continue
    }

    // Step 1: Check if file belongs to this handler
    if !isDeleteEvent && extension == ".go" {
        isMine, herr = h.depFinder.ThisFileIsMine(handler.MainInputFileRelativePath(), eventName, eventType)
        // ✅ This correctly returns true for webclient/main.go
    }

    if isMine {
        // Step 2: Call handler to process file (compilation starts)
        err := handler.NewFileEvent(fileName, extension, eventName, eventType)
        // ⚠️ PROBLEM: This may return before compilation completes!
        
        if err != nil {
            goHandlerError = err
        } else {
            processedSuccessfully = true
        }
    }
}

// Step 3: Schedule browser reload
// ❌ BUG: This happens IMMEDIATELY, not after compilation
if (isGoFileEvent && goHandlerError == nil) || (!isGoFileEvent && processedSuccessfully) {
    h.scheduleReload()  // Browser reloads in 50ms
}
```

### The Core Issue

The reload decision is made based on whether `handler.NewFileEvent()` returned an error, but this doesn't guarantee compilation completion:

1. **If compilation is synchronous**: `NewFileEvent` should block until done, but the browser reload might still race with file I/O (OS writing the .wasm file to disk)

2. **If compilation is asynchronous**: `NewFileEvent` returns immediately with no error, but compilation continues in background

3. **Timing race**: Even if synchronous, the 50ms debounce in `scheduleReload()` may not be enough for large WASM files to be fully written to disk

---

## Technical Details

### Code Paths

#### 1. File Event Detection
```
devwatch/watchEvents.go:watchEvents()
  ↓
handleFileEvent(fileName, eventName, eventType, isDeleteEvent)
  ↓
godepfind.ThisFileIsMine(mainInputFileRelativePath, fileAbsPath, event)
  ✅ Returns: (true, nil) for webclient/main.go
```

#### 2. Compilation Trigger
```
devwatch/watchEvents.go:handleFileEvent()
  ↓
handler.NewFileEvent(fileName, extension, eventName, eventType)
  ↓
tinywasm/file_event.go:NewFileEvent()
  ↓
w.activeBuilder.CompileProgram()
  ↓
gobuild: Executes "go build" or "tinygo build"
  ⚠️ May return before write completes
```

#### 3. Reload Schedule
```
devwatch/watchEvents.go:handleFileEvent()
  ↓
h.scheduleReload()
  ↓
Waits 50ms, then calls h.triggerBrowserReload()
  ❌ Too early - WASM not ready
```

### Configuration Values (Example Project)

```go
// tinywasm handler config
SourceDir: "src/cmd/webclient"      // ✅ Correct
MainInputFile: "main.go"             // ✅ Correct
MainInputFileRelativePath: "src/cmd/webclient/main.go"  // ✅ Correct

// File being edited
fileAbsPath: "/home/cesar/Dev/Pkg/Mine/golite/example/src/cmd/webclient/main.go"
relativeFilePath: "src/cmd/webclient/main.go"

// Path comparison in godepfind
relativeFilePath == mainInputFileRelativePath  // ✅ TRUE - Match found
```

---

## Proposed Solutions

### Option 1: Add Compilation Completion Callback (RECOMMENDED)

Modify the flow to wait for compilation before reloading:

```go
// tinywasm/file_event.go
func (w *TinyWasm) NewFileEvent(fileName, extension, filePath, event string) error {
    // ... existing validation ...
    
    // Create a channel to wait for compilation
    done := make(chan error, 1)
    
    // Set temporary callback
    originalCallback := w.Config.Callback
    w.Config.Callback = func(err error) {
        if originalCallback != nil {
            originalCallback(err)
        }
        done <- err
    }
    
    // Start compilation
    if err := w.activeBuilder.CompileProgram(); err != nil {
        return Err("compiling to WebAssembly error: ", err)
    }
    
    // Wait for completion
    if err := <-done; err != nil {
        return Err("compilation callback error: ", err)
    }
    
    // Restore callback
    w.Config.Callback = originalCallback
    
    return nil
}
```

**Pros:**
- Clean separation of concerns
- Works for both sync and async compilation
- No timing races

**Cons:**
- Requires modification to tinywasm
- May block file watcher thread (but that's acceptable)

---

### Option 2: Add Compilation State Tracking

Add explicit compilation state to prevent premature reload:

```go
// devwatch/devwatch.go
type DevWatch struct {
    // ... existing fields ...
    
    activeCompilations sync.WaitGroup
    compilationMutex   sync.Mutex
}

// devwatch/watchEvents.go
func (h *DevWatch) handleFileEvent(fileName, eventName, eventType string, isDeleteEvent bool) {
    // ... existing code ...
    
    if isMine {
        // Track compilation start
        if isGoFileEvent {
            h.activeCompilations.Add(1)
            defer h.activeCompilations.Done()
        }
        
        err := handler.NewFileEvent(fileName, extension, eventName, eventType)
        // ... existing error handling ...
    }
    
    // Wait for all compilations before reloading
    if isGoFileEvent && goHandlerError == nil {
        go func() {
            h.activeCompilations.Wait()
            h.scheduleReload()
        }()
    } else if !isGoFileEvent && processedSuccessfully {
        h.scheduleReload()
    }
}
```

**Pros:**
- No changes to tinywasm
- Handles multiple simultaneous compilations

**Cons:**
- More complex state management
- Still relies on compilation being truly synchronous

---

### Option 3: Increase Reload Debounce Delay

Simple but unreliable fix - increase delay from 50ms to 2-3 seconds:

```go
// devwatch/watchEvents.go
func (h *DevWatch) scheduleReload() {
    const wait = 2000 * time.Millisecond  // Changed from 50ms
    // ... rest of function ...
}
```

**Pros:**
- Minimal code change
- Easy to test

**Cons:**
- ❌ Not a real fix - just masks the problem
- ❌ Delay too short: compilation not done
- ❌ Delay too long: poor developer experience
- ❌ Varies by project size and CPU speed

---

### Option 4: Poll for Output File Existence

Wait for the output file to be written before reloading:

```go
// devwatch/watchEvents.go
func (h *DevWatch) handleFileEvent(fileName, eventName, eventType string, isDeleteEvent bool) {
    // ... existing code ...
    
    if (isGoFileEvent && goHandlerError == nil) {
        // For WASM handler, wait for output file
        if wasmHandler, ok := handler.(*tinywasm.TinyWasm); ok {
            go func() {
                outputPath := wasmHandler.MainOutputFileAbsolutePath()
                h.waitForFileReady(outputPath, 5*time.Second)
                h.scheduleReload()
            }()
        } else {
            h.scheduleReload()
        }
    } else if !isGoFileEvent && processedSuccessfully {
        h.scheduleReload()
    }
}

func (h *DevWatch) waitForFileReady(path string, timeout time.Duration) bool {
    deadline := time.Now().Add(timeout)
    var lastSize int64 = -1
    
    for time.Now().Before(deadline) {
        info, err := os.Stat(path)
        if err == nil {
            // File exists, check if size is stable
            if info.Size() == lastSize && lastSize > 0 {
                // Size unchanged, file likely complete
                time.Sleep(50 * time.Millisecond) // Extra buffer
                return true
            }
            lastSize = info.Size()
        }
        time.Sleep(100 * time.Millisecond)
    }
    
    return false // Timeout
}
```

**Pros:**
- Reliable detection of file completion
- No changes to tinywasm API
- Works for any async process

**Cons:**
- Polling overhead
- Hard to detect "truly done" writing
- Timeout management complexity

---

## Recommendation

**Primary Solution:** **Option 1** (Compilation Completion Callback)

This is the cleanest architectural solution that:
1. Makes compilation completion explicit
2. Works correctly for both sync and async cases
3. Eliminates all timing races
4. Maintains clean separation between components

**Secondary Solution:** **Option 4** (Poll for Output File)

If Option 1 is too invasive, Option 4 provides a reliable fallback that:
1. Can be implemented entirely in devwatch
2. Doesn't require changes to tinywasm or gobuild
3. Reliably detects when the output file is ready

**Not Recommended:** Options 2 and 3 have reliability or maintainability issues

---

## Testing Plan

### Test Case 1: Single File Edit
1. Start golite in example project
2. Edit `src/cmd/webclient/main.go`
3. Wait for reload
4. Verify browser shows new content
5. Check that only one compilation occurred

### Test Case 2: Rapid Sequential Edits
1. Start golite in example project
2. Make 3 quick edits to main.go (within 1 second)
3. Verify only one compilation occurs (debouncing)
4. Verify final browser state matches last edit

### Test Case 3: Large Project
1. Create a complex WASM project with many dependencies
2. Edit main file
3. Verify reload waits for complete compilation
4. Measure time between save and reload

### Test Case 4: Compilation Error
1. Edit main.go with syntax error
2. Verify browser does NOT reload
3. Fix error
4. Verify browser reloads with correct code

### Test Case 5: Mixed File Types
1. Edit main.go (triggers compilation)
2. Immediately edit style.css (no compilation)
3. Verify both changes appear in browser
4. Verify compilation completed before CSS reload

---

## Additional Notes

### Why godepfind is NOT the Issue

Initial investigation focused on `godepfind.ThisFileIsMine()` because it seemed like file ownership detection might be failing. However, testing confirmed:

1. **Path matching works correctly:**
   ```bash
   rootDir: /home/cesar/Dev/Pkg/Mine/golite/example
   mainInputFileRelativePath: src/cmd/webclient/main.go
   fileAbsPath: /home/cesar/Dev/Pkg/Mine/golite/example/src/cmd/webclient/main.go
   relativeFilePath: src/cmd/webclient/main.go
   ✓ Match! This is the handler's main file
   ```

2. **Function returns correct value:** `(true, nil)` for the webclient main file

3. **Compilation is triggered:** The issue is timing, not detection

### Related Issues

This bug may be related to:
- Fast file saves (editor writes file quickly)
- OS buffering (file appears ready but isn't fully flushed)
- Large WASM files (take longer to write)
- Slow compilation (TinyGo modes)

### Impact Assessment

**Severity:** HIGH
- Affects all WASM development workflows
- Causes confusion and wasted time
- May lead developers to think their changes didn't apply

**Frequency:** ALWAYS
- Reproduces 100% of the time for WASM files
- Only affects Go files in webclient directory
- Server and asset files work correctly

---

## Implementation Checklist

- [ ] Choose solution approach (Option 1 or 4)
- [ ] Implement changes in relevant packages
- [ ] Add unit tests for compilation completion detection
- [ ] Add integration tests for full reload flow
- [ ] Update documentation
- [ ] Test with example project
- [ ] Test with real-world projects
- [ ] Performance benchmarking
- [ ] Create migration guide if API changes

---

## References

- File: `devwatch/watchEvents.go` - handleFileEvent() function (lines 125-163)
- File: `tinywasm/file_event.go` - NewFileEvent() function (lines 16-52)
- File: `godepfind/godepfind.go` - ThisFileIsMine() function (lines 64-121)
- Example project: `golite/example/`
- Architecture: `golite/example/README.md`

---

**Document Version:** 1.0  
**Date:** 2025-10-20  
**Author:** AI Analysis  
**Next Steps:** Awaiting approval or feedback for solution implementation
