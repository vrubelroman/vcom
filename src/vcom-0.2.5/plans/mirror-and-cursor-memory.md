# Feature Plan: Mirror Pane & Per-Directory Cursor Memory

## Feature 1: Mirror active pane directory to opposite pane

### Keybinding research
- **Midnight Commander (MC)**: `Alt-i` — makes the other panel equal to current directory
- **Total Commander**: `Ctrl-PgDn` opens directory under cursor in opposite panel
- **Far Manager**: `Ctrl-Left/Right` — opens in opposite panel

**Recommendation**: Bind to `p` (for "Pane"). `Ctrl-i` sends the same byte as `Tab` (ASCII 0x09) so it conflicts with Switch. `p` is free, accessible, and "Pane" reflects the meaning.

### How it works
1. Read active pane's current path (including remote mount state)
2. Apply that path to the passive (opposite) pane
3. If active is in a remote mount, also clone the remote mount stack to the passive pane
4. Reload both panes to reflect the change

### Code changes

#### [`internal/ui/keymap.go`](internal/ui/keymap.go)
- Add `Mirror` key binding field to `KeyMap` struct
- Bind to `"p"` with help text `"p"` / `"mirror pane"`

#### [`internal/ui/model.go`](internal/ui/model.go)
- Add handler `handleMirrorPane()`:
  1. Get active pane path and remote mount state
  2. Switch passive pane to match (copy remote stack if applicable)
  3. Reload passive pane (using `reloadPane` or `reloadRemotePane`)
  4. Set status: `"Mirrored: <path>"`
- Add key match case before the `default` switch

---

## Feature 2: Per-directory cursor position memory (session scope)

### Current state
- `enterSelected()` saves `pane.Path` to history for back-nav, then reloads target dir with `selected.Name` as preserve key — but `selected.Name` is the name of the directory being entered, not a cursor anchor
- `goParent()` pops history and reloads parent using the child directory name as preserve — cursor lands on the directory we came from
- **Missing**: When navigating back into a previously-visited directory, there's no saved cursor position for it

### Design
Add a `cursorMemory` field to `BrowserPane` — a `map[string]string` mapping **directory path → last selected entry display name**.

This integrates cleanly with the existing `SetEntries(entries, preserveKey)` / `FindSelected()` infrastructure.

### Flow

```
enterSelected() / enterRemoteDir():
  1. Save to cursorMemory: pane.Path → selected entry's Name
  2. Push history (existing)
  3. Set path, reload (existing)
  4. The reload already uses preserve=selected.Name for the new dir,
     but cursorMemory will help when coming back later

reloadPane() / reloadRemotePane():
  1. Try preserve key first (existing behavior)
  2. If preserve is empty, check cursorMemory[pane.Path]
     and use that as preserveKey instead
```

### Code changes

#### [`internal/ui/pane.go`](internal/ui/pane.go)
- Add field to `BrowserPane`: `CursorMemory map[string]string`
- Method `SaveCursor(path string, name string)` — stores cursor position
- Method `LoadCursor(path string) string` — retrieves saved cursor

#### [`internal/ui/model.go`](internal/ui/model.go)
- In `enterSelected()` (line ~1886): after `pane.PushHistory(pane.Path)`, add `pane.SaveCursor(pane.Path, selected.Name)`
- In `enterRemoteDir()` (line ~5726): same save logic
- In `goParent()`: save cursor before navigating away (already partially done via history)
- In `reloadPane()` (line ~1672): after existing preserve logic, if no preserve key and directory has cursor memory, use it
- In `reloadRemotePane()` (line ~5535): same fallback

---

## Files modified
| File | Changes |
|------|---------|
| `internal/ui/keymap.go` | Add `Mirror` field, binding `p` |
| `internal/ui/pane.go` | Add `CursorMemory` field + methods |
| `internal/ui/model.go` | Add `handleMirrorPane()`, save/restore cursor in navigation methods |

## Not changed
- `internal/fs/` — storage layer unaffected
- `internal/config/` — no config changes needed
- `internal/theme/` — no theme changes needed
