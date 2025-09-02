# DevWatch initial registration: problem statement

Problem summary

`DevWatch.InitialRegistration()` currently walks the application root and notifies registered handlers only for certain file types. In the present code path it notifies `TinyWasm` only for `.go` files. That means `TinyWasm` can miss information about JavaScript assets present at startup (for example `main.js`), which leads to incorrect runtime assumptions when `TinyWasm` later provides WASM initialization JavaScript to `AssetMin`.

Consequences

- If `TinyWasm` is not informed about the JS assets present during initial registration, it may choose or cache a `wasm_exec.js` runtime shim that does not match the actual runtime shim embedded in the project's `main.js` (or vice versa).
- This mismatch can cause WebAssembly instantiation failures in the browser (missing/incorrect imports such as `runtime.scheduleTimeoutEvent` vs `runtime.sleepTicks`).

Why this is a problem

- The development orchestrator (`godev` / `devwatch`) is responsible for a consistent initial state for all handlers. If initial registration does not deliver the same event stream to `TinyWasm` as it does for other asset handlers, `TinyWasm` lacks the necessary context to select the correct runtime JS.

What must be true for a fix (high level)

- On startup, `TinyWasm` must receive the same initial file events for relevant JS assets that other asset handlers receive, so it can detect the active JS runtime configuration and avoid caching an incompatible `wasm_exec.js`.

## Proposed solution

Unify the current `FileEventAssets` and `FilesEventGO` handler types into a single interface `FilesEventHandlers` that can handle any file extension. This simplifies configuration and ensures all handlers receive the same event stream during initial registration and runtime watching.

### API changes

**New unified interface:**
```go
type FilesEventHandlers interface {
    MainInputFileRelativePath() string // eg: go => "app/server/main.go" | js =>"app/pwa/public/main.js"
    // event: create, remove, write, rename
    NewFileEvent(fileName, extension, filePath, event string) error
    SupportedExtensions() []string // eg: [".go"], [".js",".css"], etc.
    UnobservedFiles() []string // eg: main.exe, main.js
}
```

**Remove:**
- `GoFileHandler` interface
- `FileEventAssets` field in `DevWatch`
- `FilesEventGO` field in `DevWatch`

**Add:**
- `FilesEventHandlers []FilesEventHandlers` field in `DevWatch`

**Rename:**
- `AddSupportedAssetsExtensions() => addSupportedAssetsExtensions()` (internal use only)

### Implementation changes

**DevWatch.InitialRegistration():**
- Iterate through all `FilesEventHandlers` in registration order
- For each file, iterate through handlers and check if handler supports file extension via `SupportedExtensions()`
- If handler supports the file extension:
  - If extension is `.go`: use `depFinder.ThisFileIsMine()` to verify ownership before calling `NewFileEvent()`
  - If extension is not `.go`: call `NewFileEvent()` directly
- Multiple handlers can process the same file if they both support the extension

**DevWatch.handleFileEvent():**
- Replace current split logic (assets vs Go files) with unified handler iteration
- For each file event, iterate through all handlers in order
- Check each handler's `SupportedExtensions()` for the file extension
- Apply dependency analysis (`depFinder.ThisFileIsMine()`) only for handlers that support `.go` extension
- Process handlers sequentially in registration order, not in parallel

**GoLDev section-build.go:**
- Replace:
  ```go
  FileEventAssets: h.assetsHandler,
  FilesEventGO: []devwatch.GoFileHandler{h.serverHandler, h.wasmHandler}
  ```
- With:
  ```go
  FilesEventHandlers: []devwatch.FilesEventHandlers{h.assetsHandler, h.serverHandler, h.wasmHandler}
  ```
- Handler order matters and will be preserved during processing

### Benefits
- Unified event handling eliminates code duplication
- All handlers receive consistent event streams during initial registration and runtime
- TinyWasm will now receive JS events during initial registration, solving the original wasm_exec.js mismatch issue
- Simplified `UnobservedFiles` configuration (each handler declares its own)
- Easier to add new handler types
- Clear processing order based on registration sequence

### Decisions made

1. **No backward compatibility** - Clean break from current interfaces
2. **Extension handling** - All handlers supporting an extension will be notified for that file type
3. **Dependency analysis** - Only applied to handlers that support `.go` extension  
4. **Processing order** - Sequential processing in handler registration order (not parallel)
5. **Handler examples**:
   - `AssetMin.SupportedExtensions()`: `[".css", ".js", ".html", ".svg"]`
   - `TinyWasm.SupportedExtensions()`: `[".go", ".js"]` (Go for compilation, JS for runtime detection)
   - `GoServer.SupportedExtensions()`: `[".go"]`

### Remaining questions and observations

**Decisions made for pending questions:**

1. **MainInputFileRelativePath() for AssetMin:** Will implement the method but return value not relevant for DevWatch - used by godepfinder. AssetMin can return any appropriate value.

2. **supportedAssetsExtensions removal:** Correct - DevWatch's `supportedAssetsExtensions` slice will be removed since each handler declares its own extensions.

3. **addDirectoryToWatcher() update:** Will be updated to use new unified handler logic.

4. **Multiple handlers for same extension:** Yes, both TinyWasm and AssetMin will process `.js` files in sequence (order matters).

5. **triggerBrowserReload() preservation:** Browser reload behavior will be preserved with new logic.

**Final implementation details:**

- `SupportedExtensions()` implementation: Static method returning fixed list per handler type
- Processing flow: Check `SupportedExtensions()` first, then apply `depFinder.ThisFileIsMine()` only for `.go` handlers
- Error handling: Continue processing other handlers even if one fails
- Handler validation: No validation needed - DevWatch simply iterates and notifies matching handlers
- Performance: Prioritize simplicity over micro-optimizations

**SupportedExtensions() method explanation:**

This method allows each handler to declare which file extensions it can process. DevWatch uses this to determine which handlers should be notified for each file event.

Example implementations:
```go
// AssetMin - handles asset files
func (a *AssetMin) SupportedExtensions() []string {
    return []string{".css", ".js", ".html", ".svg"}
}

// TinyWasm - handles Go for compilation and JS for runtime detection
func (w *TinyWasm) SupportedExtensions() []string {
    return []string{".go", ".js"}
}

// GoServer - only handles Go files
func (s *GoServer) SupportedExtensions() []string {
    return []string{".go"}
}
```

Processing logic in DevWatch:
```go
extension := filepath.Ext(filePath) // e.g., ".js"
for _, handler := range h.FilesEventHandlers {
    if slices.Contains(handler.SupportedExtensions(), extension) {
        // This handler can process this file type
        if extension == ".go" {
            // For Go files, verify dependencies first
            if isMine, _ := h.depFinder.ThisFileIsMine(...); isMine {
                handler.NewFileEvent(...)
            }
        } else {
            // For other files, notify directly
            handler.NewFileEvent(...)
        }
    }
}
```

With this approach:
- When `file.js` changes: both AssetMin and TinyWasm receive notifications
- When `file.go` changes: only the Go handler that owns the file (determined by depFinder) receives notification
- When `file.css` changes: only AssetMin receives notification

**Code consistency verified:**
- Current `DevWatch.supportedAssetsExtensions` will be removed
- `addDirectoryToWatcher()` will use new unified logic  
- `triggerBrowserReload()` behavior preserved for all file types
- Handler registration order in `section-build.go` will be maintained during processing

**Testing strategy:**
- Verify TinyWasm receives JS events during InitialRegistration
- Confirm original wasm_exec.js mismatch issue is resolved
- Ensure all existing file watching functionality continues working
- Test handler processing order preservation

**Unit testing requirements:**

**New tests to create:**

1. **DevWatch.InitialRegistration() tests:**
   - Test with multiple handlers supporting different extensions
   - Test with handlers supporting overlapping extensions (e.g., both AssetMin and TinyWasm for `.js`)
   - Test Go file processing with `depFinder.ThisFileIsMine()` logic
   - Test non-Go file processing (direct notification)
   - Test handler processing order preservation
   - Test error handling when one handler fails but others continue

2. **DevWatch.handleFileEvent() tests:**
   - Test unified handler iteration for different file types
   - Test `SupportedExtensions()` filtering logic
   - Test dependency analysis only applied to `.go` handlers
   - Test sequential processing in registration order
   - Test `triggerBrowserReload()` preservation for all file types

3. **FilesEventHandlers interface tests:**
   - Mock handlers implementing the new interface
   - Test `SupportedExtensions()` method behavior
   - Test `MainInputFileRelativePath()` method (even if not used by DevWatch)
   - Test `UnobservedFiles()` method integration
   - Test `NewFileEvent()` method with different file types and events

**Mock handlers for testing:**

Create mock implementations of `FilesEventHandlers` interface:
```go
type MockAssetHandler struct {
    supportedExts []string
    receivedEvents []FileEvent
}

type MockGoHandler struct {
    supportedExts []string
    mainInputFile string
    receivedEvents []FileEvent
}

type MockTinyWasmHandler struct {
    supportedExts []string
    receivedEvents []FileEvent
    jsRuntimeDetected string
}
```

**Existing tests adaptation:**

1. **Update all current DevWatch tests:**
   - Replace `FileEventAssets` and `FilesEventGO` usage with `FilesEventHandlers`
   - Update test setup to use unified handler registration
   - Ensure all existing functionality tests continue passing
   - Adapt mock objects to implement new interface

2. **Update integration tests:**
   - Verify end-to-end file watching with new handler system
   - Test real handlers (AssetMin, GoServer, TinyWasm) with new interface
   - Ensure browser reload functionality preserved
   - Test initial registration with actual project structure

3. **Update handler-specific tests:**
   - AssetMin tests: implement `SupportedExtensions()` and other new methods
   - GoServer tests: adapt to new interface methods
   - TinyWasm tests: add JS file processing tests for runtime detection

**Test validation criteria:**

- All existing tests must pass after refactoring
- New tests must cover all use cases presented in examples
- Test coverage should not decrease from current levels
- Integration tests must verify the original wasm_exec.js mismatch issue is resolved
- Performance tests should confirm no significant regression in file processing speed

**Test execution order:**

1. Run existing tests with new interface (should fail initially)
2. Implement new interface in all handlers
3. Update existing tests to use new interface
4. Create new unit tests for new functionality
5. Run full test suite to ensure 100% pass rate
6. Run integration tests to verify end-to-end functionality

This document records the problem, final approved solution, and comprehensive testing strategy ready for implementation.

Filed by: developer request  
Date: 2025-09-02
