# vcom

`vcom` is a terminal file manager inspired by Midnight Commander and built with Charm's TUI stack.

## Why vcom

- Two-pane workflow focused on keyboard speed
- Built-in preview/info pane for the active selection
- Asynchronous copy/move with progress and background mode
- Theme support and configurable layout/columns

## Interface

`vcom` has two browser panes:

- active pane: navigation and file operations
- passive pane: target pane for copy/move operations

The preview/info mode (`F9` / `i`) temporarily replaces the inactive pane and shows:

- directory listing preview
- text file preview with syntax highlighting
- image metadata (format + dimensions)
- safe fallback for binary files

## Features

- Two-pane file browser
- Preview/info pane with metadata block
- Read-only view mode (`F3` / `v`)
- External editor support (`F4` / `e`)
- Rename (`F2` / `r`)
- Copy (`F5` / `c`), move (`F6` / `m`), mkdir (`F7` / `n`), delete (`F8` / `x`)
- Multi-selection and batch operations
- Copy/move progress modal with background mode
- Directory size calculation (`Space`)
- Sorting cycle (`s`) and hidden files toggle (`.`)
- Runtime theme cycle (`t`)

## Default keys

- `Tab` / `h` / `l`: switch active pane
- `j` / `Down`: move down
- `k` / `Up`: move up
- `Shift+Down` / `J`: extend selection down
- `Shift+Up` / `K`: extend selection up
- `Enter` / `Right`: open selected entry
- `Backspace` / `Left`: go to parent directory
- `Esc`: clear marked entries / close current modal
- `Ctrl+r`: refresh both panes
- `F1` / `?`: help
- `F2` / `r`: rename selected entry
- `F3` / `v`: view selected file (read-only mode)
- `F4` / `e`: edit selected file
- `F5` / `c`: copy
- `F6` / `m`: move
- `F7` / `n`: create directory
- `F8` / `x`: delete
- `F9` / `i`: toggle info/preview pane
- `F10` / `q`: quit

## Build and run

Run directly:

```bash
go run ./cmd/vcom
```

Build local binary:

```bash
go build -o vcom ./cmd/vcom
```

## Installation

### NixOS / Nix

Run directly from the flake:

```bash
nix run github:vrubelroman/vcom?ref=v0.1.2
```

Install into user profile:

```bash
nix profile add github:vrubelroman/vcom?ref=v0.1.2
```

### Debian / Ubuntu

Download the release `.deb` for `v0.1.2`, then install:

```bash
sudo apt install ./vcom_0.1.2_amd64.deb
```

### Arch Linux

A `PKGBUILD` is included in the repository:

```bash
makepkg -si
```

## Configuration

Optional config lookup order:

1. `-config /path/to/vcom.toml`
2. `./vcom.toml`
3. `./config/vcom.toml`
4. `$XDG_CONFIG_HOME/vcom/vcom.toml`
5. `~/.config/vcom/vcom.toml`

Reference config: [vcom.toml](/home/vrubel/projects/vcom/vcom.toml)

## Themes

Built-in themes:

- `catppuccin-mocha` (default)
- `tokyo-night`
- `gruvbox-dark`
- `nord-frost`

## Releases

Pushing a tag like `v0.1.2` triggers GitHub Actions release workflow (`.github/workflows/release.yml`) which:

- runs tests
- vendors Go modules
- builds release binary
- builds Debian package
- publishes release assets

Release artifacts:

- `vcom-v0.1.2-x86_64-unknown-linux-gnu.tar.gz`
- `vcom_0.1.2_amd64.deb`
- `vcom-v0.1.2-checksums.txt`

## Notes

- File creation time depends on filesystem/OS support; unavailable values are shown as `n/a`.
- Inline image rendering is intentionally disabled for now due to terminal compatibility differences.

Architecture notes: [docs/architecture.md](/home/vrubel/projects/vcom/docs/architecture.md)
