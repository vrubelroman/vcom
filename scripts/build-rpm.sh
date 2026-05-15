#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 1
fi

version="$1"
pkgname="vcom"
outdir="target/rpm"
buildroot="${outdir}/BUILDROOT"
rpmbuild_dir="${outdir}/rpmbuild"

rm -rf "$outdir"
mkdir -p \
  "${buildroot}/usr/bin" \
  "${buildroot}/usr/share/doc/vcom" \
  "${buildroot}/usr/share/licenses/vcom" \
  "${rpmbuild_dir}"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}

install -Dm755 "target/release/vcom" "${buildroot}/usr/bin/vcom"
install -Dm644 "README.md" "${buildroot}/usr/share/doc/vcom/README.md"
install -Dm644 "vcom.toml" "${buildroot}/usr/share/doc/vcom/vcom.toml"
install -Dm644 "LICENSE" "${buildroot}/usr/share/licenses/vcom/LICENSE"

cat > "${rpmbuild_dir}/SPECS/vcom.spec" <<EOF
Name:           vcom
Version:        ${version}
Release:        1%{?dist}
Summary:        Terminal file manager inspired by Midnight Commander
License:        GPLv3
URL:            https://github.com/vrubelroman/vcom

BuildArch:      x86_64
Recommends:     ueberzugpp

%description
A two-pane terminal file manager with inspect mode and text previews.

%files
%dir /usr/share/doc/vcom
%dir /usr/share/licenses/vcom
/usr/bin/vcom
/usr/share/doc/vcom/README.md
/usr/share/doc/vcom/vcom.toml
/usr/share/licenses/vcom/LICENSE

%changelog
EOF

rpmbuild \
  --define "_topdir $(realpath "${rpmbuild_dir}")" \
  --define "_buildroot ${buildroot}" \
  --buildroot "$(realpath "${buildroot}")" \
  -bb \
  --noclean \
  "${rpmbuild_dir}/SPECS/vcom.spec"

rpm_path=$(find "${rpmbuild_dir}/RPMS" -name "*.rpm" | head -1)
cp "$rpm_path" "${outdir}/${pkgname}-${version}-1.x86_64.rpm"
echo "Built: ${outdir}/${pkgname}-${version}-1.x86_64.rpm"
