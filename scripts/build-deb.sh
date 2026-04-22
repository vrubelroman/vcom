#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 1
fi

version="$1"
pkgroot="target/debian/pkgroot"
outdir="target/debian"

rm -rf "$pkgroot"
mkdir -p \
  "$pkgroot/DEBIAN" \
  "$pkgroot/usr/bin" \
  "$pkgroot/usr/share/doc/vcom" \
  "$pkgroot/usr/share/licenses/vcom"

install -Dm755 "target/release/vcom" "$pkgroot/usr/bin/vcom"
install -Dm644 "README.md" "$pkgroot/usr/share/doc/vcom/README.md"
install -Dm644 "vcom.toml" "$pkgroot/usr/share/doc/vcom/vcom.toml"
install -Dm644 "LICENSE" "$pkgroot/usr/share/licenses/vcom/LICENSE"

cat > "$pkgroot/DEBIAN/control" <<EOF
Package: vcom
Version: ${version}
Section: utils
Priority: optional
Architecture: amd64
Maintainer: Roman Vrubel <roman@vrubel.dev>
Description: Terminal file manager inspired by Midnight Commander
 A two-pane terminal file manager with inspect mode and text previews.
EOF

dpkg-deb --build "$pkgroot" "$outdir/vcom_${version}_amd64.deb"
