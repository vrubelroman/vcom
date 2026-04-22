# vcom

`vcom` is a terminal file manager inspired by Midnight Commander and built on top of Charm's TUI stack.

The layout is:

- left browser pane
- right browser pane

The key difference from classic `mc` is the inspect mode on `i`. It temporarily replaces the inactive pane with a preview/info panel for the active side selection:

- directory: child entries
- text file: text preview
- image: metadata and dimensions
- binary or unsupported file: safe fallback preview

## Goals

- MC-like browsing and function-key workflow
- configurable column layout
- pleasant default styling
- readable, editable config with commented-out optional features
- clean architecture for async file operations and previews

## Current feature set

- MC-like two-pane layout by default
- preview/info mode on `i`
- left/right file browsers
- preview with top metadata widget
- themes with `catppuccin-mocha` as default
- runtime theme cycling with `t`
- configurable visible columns
- hidden files shown by default
- hidden files toggle with `.`
- browser sort cycling with `s`
- directory size calculation on `Space`
- overwrite confirmation before copy or move
- pager on `F3` and editor on `F4` when available
- basic `F5` copy, `F6` move, `F7` mkdir, `F8` delete, `F10` quit

## Controls

- `Tab`: switch active browser pane
- `Enter`: open directory
- `Backspace` or `Left`: go to parent directory
- `i`: replace inactive pane with info/preview for the active pane selection
- `Space`: calculate selected directory size
- `.`: toggle hidden entries
- `s`: cycle sort mode
- `t`: cycle theme
- `F3`: open selected file in `$PAGER` if available, otherwise stay in center preview
- `F4`: open selected file in `$EDITOR`
- `F5`: copy to passive pane path
- `F6`: move to passive pane path
- `F7`: create directory in active pane
- `F8`: delete selected entry
- `F10`: quit

## Run

```bash
go run ./cmd/vcom
```

To build a local binary:

```bash
go build -o vcom ./cmd/vcom
```

Optional config file lookup order:

1. `-config /path/to/vcom.toml`
2. `./vcom.toml`
3. `./config/vcom.toml`
4. `$XDG_CONFIG_HOME/vcom/vcom.toml`
5. `~/.config/vcom/vcom.toml`

## Themes

Built-in presets:

- `catppuccin-mocha`
- `tokyo-night`
- `gruvbox-dark`
- `nord-frost`

The sample config in [vcom.toml](/home/vrubel/projects/vcom/vcom.toml) is intentionally written so optional columns and sort variants can be uncommented by hand.

## Notes

- File creation time depends on filesystem and OS support. When unavailable the UI shows `n/a`.
- Real inline image rendering is intentionally not enabled yet because terminal support is fragmented. The current architecture keeps that as an isolated preview backend to add later.

More detail is in [docs/architecture.md](/home/vrubel/projects/vcom/docs/architecture.md).

## Installation

### NixOS / Nix

Run directly from the flake:

```bash
nix run github:vrubelroman/vcom?ref=<tag>
```

Install into the current profile:

```bash
nix profile add github:vrubelroman/vcom?ref=<tag>
```

`nix profile add` installs `vcom` into the current user's Nix profile. It does not edit `configuration.nix`, does not rebuild NixOS, and does not make the package a declarative system package.

### Debian / Ubuntu

Release builds include a `.deb` artifact. Install it with:

```bash
sudo apt install ./vcom_<version>_amd64.deb
```

### Arch Linux

A `PKGBUILD` is included in the repository. Build it with:

```bash
makepkg -si
```

## Releases

Tagging a version like `v0.1.0` triggers the GitHub Actions release workflow:

- runs `go test ./...`
- vendors Go modules for reproducible packaging
- builds the release binary
- builds the `.deb` package with `dpkg-deb`
- publishes release artifacts to GitHub Releases

Release artifacts include:

- `vcom-<tag>-x86_64-unknown-linux-gnu.tar.gz`
- `vcom_<version>_amd64.deb`
- `vcom-<tag>-checksums.txt`
