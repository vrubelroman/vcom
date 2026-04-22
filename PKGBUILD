pkgname=vcom
pkgver=0.1.0
pkgrel=1
pkgdesc="Terminal file manager inspired by Midnight Commander"
arch=("x86_64" "aarch64")
url="https://github.com/vrubelroman/vcom"
license=("MIT")
depends=("glibc")
makedepends=("go")
source=("$pkgname-$pkgver.tar.gz::$url/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=("SKIP")

build() {
  cd "$srcdir/$pkgname-$pkgver"
  export CGO_ENABLED=0
  export GOFLAGS="-mod=vendor -trimpath"
  go build -ldflags="-s -w" -o "target/release/vcom" ./cmd/vcom
}

check() {
  cd "$srcdir/$pkgname-$pkgver"
  export CGO_ENABLED=0
  export GOFLAGS="-mod=vendor"
  go test ./...
}

package() {
  cd "$srcdir/$pkgname-$pkgver"

  install -Dm755 "target/release/vcom" "$pkgdir/usr/bin/vcom"
  install -Dm644 "README.md" "$pkgdir/usr/share/doc/vcom/README.md"
  install -Dm644 "vcom.toml" "$pkgdir/usr/share/doc/vcom/vcom.toml"
  install -Dm644 "LICENSE" "$pkgdir/usr/share/licenses/vcom/LICENSE"
}
