# Architecture

## Why this shape

The project should not become a giant `main.go` that mixes rendering, key handling, filesystem I/O and business rules. The architecture is split so the UI remains reactive and file operations stay isolated.

## High-level modules

- `cmd/vcom`
  Entry point, config loading, program startup.
- `internal/config`
  Config schema, defaults, search paths and TOML parsing.
- `internal/fs`
  Filesystem model, directory scanning, previews and file operations.
- `internal/theme`
  Theme presets and style tokens.
- `internal/ui`
  Bubble Tea model, key map, pane rendering, modal flow and layout.

## State model

The Bubble Tea root model owns:

- terminal dimensions
- full config
- active browser pane
- left and right pane state
- center preview state
- transient modal state
- busy/status state for async work

This keeps the Elm-style update loop simple:

1. key or resize event arrives
2. model decides whether to mutate local state or start async work
3. async work returns a typed message
4. view renders from plain state

## Pane model

Each side pane stores:

- current path
- scanned entries
- cursor index
- scroll offset
- cached directory sizes for entries calculated on demand

This is intentionally independent from rendering details, so the pane can be unit-tested later without Lip Gloss.

## Preview pipeline

The center pane is driven only by the current selection from the active side pane.

Preview strategies:

- directory preview
  Shows child entries and summary data.
- text preview
  Reads up to a configured byte limit and displays text safely.
- image preview
  Detects dimensions and format through standard Go image decoders.
- binary fallback
  Avoids dumping junk bytes into the terminal.

Metadata shown above the preview:

- kind
- full path
- size
- created time
- modified time
- permissions

Directory size is expensive, so it is not calculated eagerly. Pressing `Space` starts an async scan and updates both:

- side-pane size column
- preview metadata widget

## Configuration design

TOML is used because:

- it is readable in terminal workflows
- comments are first-class
- optional settings can stay commented out for manual enable/disable

The example config enables only MC-like columns by default:

- `name`
- `size`
- `modified`

Additional columns are left commented out so users can literally uncomment them:

- `created`
- `permissions`
- `extension`

Other useful config areas:

- startup paths for left and right panes
- theme preset
- hidden file visibility
- sort field and reverse mode
- center pane width ratio
- confirmation behavior
- preview byte limits
- text wrapping in preview
- compact path display

## Rendering strategy

The side panes are custom-rendered rather than built on top of the generic table bubble.

Reasoning:

- MC-like directory panes are not just tables
- we need tight control over selection styling, truncation and empty states
- the center pane uses a different rendering model than the side panes

Bubble usage:

- `bubbletea`
  event loop and async commands
- `bubbles/key`
  declarative key bindings
- `bubbles/textinput`
  mkdir modal
- `bubbles/viewport`
  preview scrolling surface
- `lipgloss`
  all layout and styling

## Operations

The first operational slice is intentionally MC-core:

- copy selected entry to passive pane directory
- move selected entry to passive pane directory
- create directory in active pane
- delete selected entry
- refresh active pane

These operations are dispatched asynchronously to avoid freezing the TUI on large directories.

Overwrite handling is decided before the async operation begins:

- if the target does not exist, the operation runs directly
- if the target exists and overwrite confirmation is enabled, the UI opens a modal
- if overwrite confirmation is disabled, the operation replaces the target immediately

## Theme system

Themes are token-based rather than style-object based.

Each preset defines semantic colors:

- background
- panel
- panel inactive
- border
- border active
- text
- muted text
- accent
- selection
- warning
- danger
- footer key

This allows future user-defined themes without touching UI logic.

The current UI also supports runtime theme cycling, but config remains the source of truth for default startup appearance.

## Extension points

The architecture is designed to accept later additions without a rewrite:

- multi-select and batch operations
- file viewer/editor integration
- terminal image protocols
- tab history per pane
- bookmarks and quick-jump
- sort modes
- archive browsing
- search/filter overlay
- pluggable preview providers

## Constraints worth keeping

- never calculate directory sizes during normal navigation
- never read full large files into memory for preview
- keep filesystem code out of `View()`
- keep styling decisions out of `internal/fs`
- prefer typed Tea messages over stringly-typed status events
