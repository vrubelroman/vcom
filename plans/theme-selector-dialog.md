# Plan: Theme Selector Dialog with Live Preview

## Summary
Replace the current `cycleTheme()` (simple cycle through themes on `t`) with a modal dialog that shows all themes, their base colors, supports live preview via Up/Down navigation, and commit/revert on Enter/Esc.

## Changes

### 1. `internal/ui/model.go` — New modal kind

Add to `modalKind` const (around line 34):
```go
modalThemeSelect
```

### 2. `internal/ui/model.go` — New theme selector state

Add new struct and field to `Model`:

```go
type themeSelectorState struct {
    names    []string    // all theme names in order
    cursor   int         // current cursor index in the list
    original string      // the theme name before opening dialog (for Esc revert)
}

type Model struct {
    // ... existing fields ...
    themeSelector *themeSelectorState  // nil when not in theme selector dialog
}
```

### 3. `internal/ui/model.go` — New handler `openThemeSelector()`

Replace the `cycleTheme()` call with `openThemeSelector()`:

1. Read current theme name from `m.cfg.UI.Theme`
2. Get all theme names via `theme.Names()`
3. Find current theme index in the list
4. Create `themeSelectorState` with names, cursor=current index, original=current theme
5. Set `m.modal.kind = modalThemeSelect`
6. Set modal title/body (body may be empty since list is rendered in `renderThemeSelectModal`)

The `t` key match (`case key.Matches(msg, m.keys.CycleTheme)`) now calls `m.openThemeSelector()` instead of `m.cycleTheme()`.

### 4. `internal/ui/model.go` — Modal key handling

Add a new case in `handleModalKey()` for `modalThemeSelect`:

```go
case modalThemeSelect:
    switch {
    case msg.String() == "up" || msg.String() == "k":
        // Move cursor up, clamp to 0, apply theme preview
        m.themeSelector.cursor--
        if m.themeSelector.cursor < 0 { m.themeSelector.cursor = 0 }
        m.applyThemePreview(m.themeSelector.names[m.themeSelector.cursor])
        return m, nil
    case msg.String() == "down" || msg.String() == "j":
        // Move cursor down, clamp to len-1, apply theme preview
        m.themeSelector.cursor++
        if m.themeSelector.cursor >= len(m.themeSelector.names) {
            m.themeSelector.cursor = len(m.themeSelector.names) - 1
        }
        m.applyThemePreview(m.themeSelector.names[m.themeSelector.cursor])
        return m, nil
    case key.Matches(msg, m.keys.Confirm):  // Enter
        // Apply selected theme and save
        selected := m.themeSelector.names[m.themeSelector.cursor]
        m.finalizeTheme(selected)
        m.themeSelector = nil
        m.modal = modalState{}
        m.status = fmt.Sprintf("Theme: %s", selected)
        return m, nil
    case msg.String() == "esc":
        // Revert to original theme
        m.applyThemePreview(m.themeSelector.original)
        m.themeSelector = nil
        m.modal = modalState{}
        m.status = "Theme unchanged"
        return m, nil
    }
```

### 5. `internal/ui/model.go` — applyThemePreview helper

A new method that applies a theme palette to `m.palette` **without saving to config**:

```go
func (m *Model) applyThemePreview(name string) {
    palette, err := theme.Resolve(name)
    if err != nil {
        return  // silently ignore resolve errors during preview
    }
    m.palette = palette
    // Don't update m.cfg.UI.Theme or save — that's only on Enter
}
```

### 6. `internal/ui/model.go` — finalizeTheme helper

A new method that applies the theme AND saves to config:

```go
func (m *Model) finalizeTheme(name string) {
    palette, err := theme.Resolve(name)
    if err != nil {
        m.status = err.Error()
        return
    }
    m.cfg.UI.Theme = name
    m.palette = palette
    savedPath, saveErr := config.Save(m.cfg, m.configPath)
    if saveErr != nil {
        m.status = fmt.Sprintf("Theme: %s (save failed: %v)", name, saveErr)
        return
    }
    m.configPath = savedPath
}
```

### 7. `internal/ui/model.go` — renderThemeSelectModal

Create a new render function `renderThemeSelectModal()`:

```go
func renderThemeSelectModal(m Model, palette theme.Palette, width int) string {
    outerWidth := max(width, 8)
    contentWidth := max(outerWidth-6, 1)
    
    // Styles
    titleStyle := lipgloss.NewStyle()...
    box := lipgloss.NewStyle()...
    
    // Build theme list rows
    lines := []string{titleStyle.Render("Select Theme"), spacer}
    lines = append(lines, instructions)
    lines = append(lines, spacer)
    
    for i, name := range m.themeSelector.names {
        resolved, err := theme.Resolve(name)
        // skip if error
        
        // Selection indicator + theme name
        // Color swatches: Background, Panel, Accent, Text, Selection
        // Highlight current item
    }
    
    return box.Render(strings.Join(lines, "\n"))
}
```

Each row shows:
- Cursor indicator (`▸` or ` `)
- Theme name
- Color swatches as small colored blocks: Background, Panel, Accent, Text, Selection
- Currently selected item is highlighted with `Selection` background color

### 8. `internal/ui/model.go` — Update View dispatch

Add a new condition in `renderModal()` (around line 3698) to dispatch to `renderThemeSelectModal` when `modalKind == modalThemeSelect`:

```go
if m.modal.kind == modalThemeSelect {
    return renderThemeSelectModal(m, palette, width)
}
```

### 9. `internal/ui/model.go` — Remove old cycleTheme (optional)

The old `cycleTheme()` method can be kept for backward compatibility or removed. The `t` key will now open the dialog instead.

### 10. Help dialog update (optional)

Update the help dialog to reflect the new behavior: `"t  open theme selector"` instead of `"t  cycle theme"`.

## Files modified
| File | Changes |
|------|---------|
| `internal/ui/model.go` | Add `modalThemeSelect` kind, `themeSelectorState` struct, `openThemeSelector()`, `applyThemePreview()`, `finalizeTheme()`, key handling in `handleModalKey`, `renderThemeSelectModal()`, update `renderModal()` dispatch |
| (none else) | Theme data, config saving, keymap — all remain unchanged |

## Key design points
1. **Live preview**: Every Up/Down key press instantly resolves the new theme palette and applies it to `m.palette`. The user sees the full UI change in real time.
2. **Safe revert**: On Esc, the original theme (saved in `themeSelectorState.original`) is restored.
3. **Config save only on Enter**: Only `finalizeTheme()` persists to config file.
4. **Color swatches**: Each theme row shows 5 small colored blocks (Background, Panel, Accent, Text, Selection) so the user can visually compare themes.
5. **Clean state management**: `themeSelector` is `nil` when the dialog is closed, making it easy to check state.

## Not changed
- `internal/theme/` — storage layer unchanged
- `internal/config/` — no config changes
- `internal/ui/keymap.go` — `t` binding unchanged, only behavior changes
- `internal/ui/pane.go` — no changes
