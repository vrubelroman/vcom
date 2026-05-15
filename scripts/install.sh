#!/usr/bin/env bash
set -euo pipefail

REPO="vrubelroman/vcom"
FONT_DIR="$HOME/.local/share/fonts/JetBrainsMonoNerd"
FONT_URL="https://github.com/ryanoasis/nerd-fonts/releases/latest/download/JetBrainsMono.zip"

BOLD="\033[1m"
GREEN="\033[32m"
YELLOW="\033[33m"
RESET="\033[0m"

info()  { echo -e "${GREEN}→${RESET} $*"; }
warn()  { echo -e "${YELLOW}→${RESET} $*"; }
header() { echo -e "\n${BOLD}== $* ==${RESET}"; }

# ---------- detect distro ----------
detect_distro() {
  if [ -f /etc/os-release ]; then
    . /etc/os-release
    echo "${ID}"
  elif command -v nixos-version >/dev/null 2>&1; then
    echo "nixos"
  else
    echo "unknown"
  fi
}

# ---------- latest release tag ----------
latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
    | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# ---------- nerd font ----------
install_nerd_font() {
  header "Nerd Font"
  if fc-list 2>/dev/null | grep -qi "JetBrainsMono.*Nerd"; then
    info "JetBrainsMono Nerd Font already installed"
    return
  fi
  DISTRO=$(detect_distro)
  case "$DISTRO" in
    arch|manjaro|endeavouros)
      info "Installing via pacman..."
      sudo pacman -S --noconfirm ttf-jetbrains-mono-nerd 2>/dev/null || true
      ;;
    nixos|nix)
      if command -v nix >/dev/null 2>&1; then
        info "Installing via nix profile..."
        nix profile install nixpkgs#nerd-fonts.jetbrains-mono 2>/dev/null || true
      fi
      ;;
  esac
  if fc-list 2>/dev/null | grep -qi "JetBrainsMono.*Nerd"; then
    info "JetBrainsMono Nerd Font installed via package manager"
    return
  fi
  info "Downloading JetBrainsMono Nerd Font..."
  mkdir -p "$FONT_DIR"
  curl -fsSL "$FONT_URL" -o /tmp/JetBrainsMono.zip
  unzip -oq /tmp/JetBrainsMono.zip -d "$FONT_DIR"
  rm -f /tmp/JetBrainsMono.zip
  if command -v fc-cache >/dev/null 2>&1; then
    fc-cache -fv "$FONT_DIR" 2>/dev/null || true
  fi
  info "Nerd Font installed to $FONT_DIR"
}

# ---------- ueberzugpp ----------
install_ueberzugpp() {
  header "ueberzugpp (image preview)"
  if command -v ueberzugpp >/dev/null 2>&1; then
    info "ueberzugpp already installed"
    return
  fi
  DISTRO=$(detect_distro)
  case "$DISTRO" in
    arch|manjaro|endeavouros)
      sudo pacman -S --noconfirm ueberzugpp 2>/dev/null && return || true
      ;;
    fedora|rhel|centos|almalinux|rocky)
      sudo dnf install -y ueberzugpp 2>/dev/null && return || true
      ;;
    nixos|nix)
      info "ueberzugpp is bundled with vcom on Nix — skipping"
      return
      ;;
  esac
  if ! command -v pipx >/dev/null 2>&1; then
    info "Installing pipx..."
    sudo apt-get update -qq && sudo apt-get install -y -qq pipx 2>/dev/null || \
      sudo dnf install -y pipx 2>/dev/null || \
      sudo zypper install -y pipx 2>/dev/null || true
    pipx ensurepath 2>/dev/null || true
  fi
  if command -v pipx >/dev/null 2>&1; then
    pipx install ueberzugpp 2>/dev/null && return || true
  fi
  warn "Could not install ueberzugpp — image preview may not work outside Kitty"
}

# ---------- vcom ----------
install_vcom() {
  header "vcom"
  TAG=$(latest_tag)
  if [ -z "$TAG" ]; then
    warn "Could not determine latest version from GitHub"
    return 1
  fi
  VER="${TAG#v}"
  DISTRO=$(detect_distro)

  case "$DISTRO" in
    debian|ubuntu|linuxmint|pop|elementary|zorin|kali|raspbian)
      DEB="vcom_${VER}_amd64.deb"
      URL="https://github.com/${REPO}/releases/download/${TAG}/${DEB}"
      info "Downloading $DEB for $DISTRO..."
      curl -fsSL "$URL" -o "/tmp/${DEB}"
      sudo apt install -y "/tmp/${DEB}"
      rm -f "/tmp/${DEB}"
      ;;
    fedora|rhel|centos|almalinux|rocky)
      RPM="vcom-${VER}-1.x86_64.rpm"
      URL="https://github.com/${REPO}/releases/download/${TAG}/${RPM}"
      info "Downloading $RPM for $DISTRO..."
      curl -fsSL "$URL" -o "/tmp/${RPM}"
      sudo dnf install -y "/tmp/${RPM}" 2>/dev/null || \
        sudo rpm -ivh "/tmp/${RPM}"
      rm -f "/tmp/${RPM}"
      ;;
    opensuse*|sles)
      RPM="vcom-${VER}-1.x86_64.rpm"
      URL="https://github.com/${REPO}/releases/download/${TAG}/${RPM}"
      info "Downloading $RPM for $DISTRO..."
      curl -fsSL "$URL" -o "/tmp/${RPM}"
      sudo zypper install -y "/tmp/${RPM}"
      rm -f "/tmp/${RPM}"
      ;;
    arch|manjaro|endeavouros)
      TAR="vcom-${TAG}-x86_64-unknown-linux-gnu.tar.gz"
      URL="https://github.com/${REPO}/releases/download/${TAG}/${TAR}"
      info "Downloading $TAR for $DISTRO..."
      curl -fsSL "$URL" -o "/tmp/${TAR}"
      mkdir -p "$HOME/.local/bin"
      tar -xzf "/tmp/${TAR}" -C "$HOME/.local/bin"
      rm -f "/tmp/${TAR}"
      if ! echo "$PATH" | grep -q "$HOME/.local/bin"; then
        warn "Add ~/.local/bin to your PATH:"
        echo '  export PATH="$HOME/.local/bin:$PATH"'
      fi
      ;;
    nixos|nix)
      info "Installing via nix profile..."
      nix profile add "github:${REPO}?ref=${TAG}"
      return
      ;;
    *)
      TAR="vcom-${TAG}-x86_64-unknown-linux-gnu.tar.gz"
      URL="https://github.com/${REPO}/releases/download/${TAG}/${TAR}"
      info "Unknown distro ($DISTRO) — installing binary tarball"
      curl -fsSL "$URL" -o "/tmp/${TAR}"
      mkdir -p "$HOME/.local/bin"
      tar -xzf "/tmp/${TAR}" -C "$HOME/.local/bin"
      rm -f "/tmp/${TAR}"
      warn "Add ~/.local/bin to your PATH if not already:"
      echo '  export PATH="$HOME/.local/bin:$PATH"'
      ;;
  esac
  info "vcom ${TAG} installed successfully"
}

# ---------- main ----------
echo ""
echo -e "${BOLD}vcom installer${RESET}"
echo ""

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required" >&2
  exit 1
fi
if ! command -v unzip >/dev/null 2>&1; then
  echo "error: unzip is required" >&2
  exit 1
fi

install_nerd_font
install_ueberzugpp
install_vcom

echo ""
info "Done! Run: vcom"
