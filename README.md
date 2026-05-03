# vcom

`vcom` is a two-pane terminal file manager with a fast built-in info/preview panel for the active selection.

## Why vcom

- Two-pane workflow focused on keyboard speed
- Built-in preview/info pane for the active selection
- Asynchronous copy/move with progress and background mode
- Theme support and configurable layout/columns

## Screenshots

![vcom screenshot](docs/screen.png)
![vcom screenshot 2](docs/screen2.png)
![vcom screenshot 3](docs/screen3.png)

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
nix run github:vrubelroman/vcom?ref=v0.2.4
```

Install into user profile:

```bash
nix profile add github:vrubelroman/vcom?ref=v0.2.4
```

The Nix package wraps `vcom` with `ueberzugpp` in `PATH`, so image preview works in non-`kitty` terminals out of the box.

### Debian / Ubuntu

Download and install the latest release:

```bash
curl -sL https://github.com/vrubelroman/vcom/releases/download/v0.2.4/vcom_0.2.4_amd64.deb -o /tmp/vcom_0.2.4_amd64.deb
sudo apt install /tmp/vcom_0.2.4_amd64.deb
```

The Debian package declares `ueberzug` (or `ueberzugpp` where available) as a dependency for image preview outside `kitty`.

### Arch Linux

A `PKGBUILD` is included in the repository:

```bash
makepkg -si
```

The Arch package depends on `ueberzugpp`, so non-`kitty` image preview is installed together with `vcom`.

## Font requirement (icons)

For file icons, `vcom` expects a Nerd Font in your terminal profile.

Default behavior is `ui.icon_mode = "auto"`:

- if a Nerd Font is detected, `vcom` uses Nerd icons
- if not, `vcom` falls back to ASCII icons automatically

You can force behavior in config:

- `ui.icon_mode = "nerd"`: always use Nerd icons
- `ui.icon_mode = "ascii"`: always use ASCII icons

### Installing a Nerd Font

**Ubuntu / Debian:**

```bash
wget -qO /tmp/JetBrainsMono.zip https://github.com/ryanoasis/nerd-fonts/releases/latest/download/JetBrainsMono.zip
mkdir -p ~/.local/share/fonts/JetBrainsMonoNerd
unzip -o /tmp/JetBrainsMono.zip -d ~/.local/share/fonts/JetBrainsMonoNerd
fc-cache -fv
```

**Arch Linux:**

```bash
sudo pacman -S ttf-jetbrains-mono-nerd
```

Or via AUR helper:

```bash
yay -S nerd-fonts-jetbrains-mono
```

**NixOS / Nix:**

Add to your `/etc/nixos/configuration.nix`:

```nix
fonts.packages = with pkgs; [ nerd-fonts.jetbrains-mono ];
```

Or install imperatively:

```bash
nix profile install nixpkgs#nerd-fonts.jetbrains-mono
```

### Configuring terminal to use the installed Nerd Font

After installing, set `JetBrainsMono Nerd Font` (or another Nerd Font) as the terminal font:

- **GNOME Terminal:** `Preferences → Profile → Text → Custom font` → choose `JetBrainsMono Nerd Font`
- **Konsole:** `Settings → Edit Current Profile → Appearance` → choose a Nerd Font profile
- **Alacritty:** set `font.normal.family: "JetBrainsMono Nerd Font"` in `~/.config/alacritty/alacritty.yml`
- **Kitty:** set `font_family JetBrainsMono Nerd Font` in `~/.config/kitty/kitty.conf`
- **Foot:** set `font=JetBrainsMono Nerd Font:size=11` in `~/.config/foot/foot.ini`
- **WezTerm:** set `font = wezterm.font("JetBrainsMono Nerd Font")` in `~/.config/wezterm/wezterm.lua`
- **Windows Terminal:** `Settings → Profiles → Defaults → Appearance → Font face` → choose `JetBrainsMono Nerd Font`

Preview mode (`F9` / `i`) temporarily replaces the inactive pane and shows:

- directory listing preview
- text file preview with syntax highlighting
- image metadata (format + dimensions)
- safe fallback for binary files

## Configuration

Optional config lookup order:

1. `-config /path/to/vcom.toml`
2. `./vcom.toml`
3. `./config/vcom.toml`
4. `$XDG_CONFIG_HOME/vcom/vcom.toml`
5. `~/.config/vcom/vcom.toml`

Reference config: [vcom.toml](/home/vrubel/projects/vcom/vcom.toml)

Icon mode example:

```toml
[ui]
icon_mode = "auto" # auto | nerd | ascii
```

## Themes

Built-in themes (press `t` to open theme selector or set `ui.theme` in config):

- `catppuccin-mocha` (default)
- `catppuccin-macchiato`
- `catppuccin-lavender`
- `tokyo-night`
- `gruvbox-dark`
- `nord`
- `one-dark`
- `everforest`
- `github-dark`
- `ayu-dark`
- `breeze`
- `cyberpunk`
- `dracula`
- `eldritch`
- `kanagawa`
- `kanagawa-paper`
- `rose-pine`
- `solarized-dark`
- `vesper`

## Releases

Pushing a tag like `v0.2.4` triggers GitHub Actions release workflow (`.github/workflows/release.yml`) which:

- runs tests
- vendors Go modules
- builds release binary
- builds Debian package
- publishes release assets

Release artifacts:

- `vcom-v0.2.4-x86_64-unknown-linux-gnu.tar.gz`
- `vcom_0.2.4_amd64.deb`
- `vcom-v0.2.4-checksums.txt`

## Notes

- File creation time depends on filesystem/OS support; unavailable values are shown as `n/a`.

Architecture notes: [docs/architecture.md](/home/vrubel/projects/vcom/docs/architecture.md)
