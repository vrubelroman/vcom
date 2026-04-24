# vcom

`vcom` is a two-pane terminal file manager with a fast built-in info/preview panel for the active selection.

## Why vcom

- Two-pane workflow focused on keyboard speed
- Built-in preview/info pane for the active selection
- Asynchronous copy/move with progress and background mode
- Theme support and configurable layout/columns

Preview mode (`F9` / `i`) temporarily replaces the inactive pane and shows:

- directory listing preview
- text file preview with syntax highlighting
- image metadata (format + dimensions)
- safe fallback for binary files

## Screenshot

![vcom screenshot](docs/screen.png)

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
