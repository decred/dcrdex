#!/usr/bin/env bash

# A good getting-started guide for Debian packaging can be found at
# https://www.internalpointers.com/post/build-binary-deb-package-practical-guide

# turn this on for debugging, keep noise low for prod builds
# set -ex

APP="bisonw"
VER="1.0.5"
META="release"
REV="0"
ARCH="amd64"

# A directory containing metadata files
SRC_DIR="./src"

# The DEB_DIR represents the root directory in our target system. The directory
# structure created under DEB_DIR will be duplicated on the target system
# during installation.
DEB_DIR="${BUILD_DIR}/${DEB_NAME}"

# Magic name for a directory containing a specially formatted control file
# (is it INI?) and special scripts to run during installation.
CONTROL_DIR="${DEB_DIR}/DEBIAN"

# posinst/postrm are special filenames under the DEBIAN directory. The scripts
# will be run after installation/uninstall.
POSTINST_PATH="${CONTROL_DIR}/postinst"
POSTRM_PATH="${CONTROL_DIR}/postrm"

# The dexc binary.
BIN_TARGETDIR="/usr/bin"
BIN_BUILDDIR="${DEB_DIR}${BIN_TARGETDIR}"
BIN_FILENAME="${APP}"
BIN_BUILDPATH="${BIN_BUILDDIR}/${BIN_FILENAME}"

# The Desktop Entry is a format for "installing" programs on Linux, creating
# an entry in the main menu.
# https://specifications.freedesktop.org/desktop-entry-spec/latest/
DOT_DESKTOP_TARGETDIR="/usr/share/applications"
DOT_DESKTOP_BUILDDIR="${DEB_DIR}${DOT_DESKTOP_TARGETDIR}"
DOT_DESKTOP_FILENAME="bisonw.desktop"
DOT_DESKTOP_BUILDPATH="${DOT_DESKTOP_BUILDDIR}/${DOT_DESKTOP_FILENAME}"

# Prepare the directory structure.
rm -fr "${BUILD_DIR}"
mkdir -p -m 0755 "${CONTROL_DIR}"
mkdir -p "${DOT_DESKTOP_BUILDDIR}"

# Build site bundle
CWD=$(pwd)
cd ../../webserver/site
npm clean-install
npm run build
cd $CWD

# Build bisonw
LDFLAGS="-s -w -X main.Version=${VER}${META:++${META}}"
GOOS=linux GOARCH=${ARCH} go build -o "${BIN_BUILDPATH}" -ldflags "$LDFLAGS"
chmod 755 "${BIN_BUILDPATH}"

# Full control file specification -
# https://www.debian.org/doc/debian-policy/ch-controlfields.html
cat > "${CONTROL_DIR}/control" <<EOF
Package: bisonw
Version: ${VER}
Architecture: ${ARCH}
Maintainer: Decred developers <briantstafford@gmail.com>
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: A multi-wallet backed by Decred DEX
EOF

# Copy icons
# This will be the icon shown for the program in the taskbar. I know that both
# PNG and SVG will work. If it's a bitmap, should probably be >= 128 x 128 px.
ICON_TARGETDIR="/usr/share/icons/hicolor"
ICON_BUILDDIR="${DEB_DIR}${ICON_TARGETDIR}"
install -Dm644 -t "${ICON_BUILDDIR}/scalable/apps" "${SRC_DIR}/bisonw.svg"
install -Dm644 -t "${ICON_BUILDDIR}/128x128/apps" "${SRC_DIR}/bisonw.png"

# AppStream metadata
# https://wiki.debian.org/AppStream
install -Dm644 -t "${DEB_DIR}/usr/share/metainfo" "${SRC_DIR}/org.decred.dcrdex.metainfo.xml"

# Update the desktop icons, refresh the "start" menu.
cat > "${POSTINST_PATH}" <<EOF
update-desktop-database
EOF
chmod 775 "${POSTINST_PATH}"

# Example file:
# https://specifications.freedesktop.org/desktop-entry-spec/latest/apa.html
cat > "${DOT_DESKTOP_BUILDPATH}" <<EOF
[Desktop Entry]
Version=1.5
Name=Bison Wallet
Comment=Multi-wallet backed by Decred DEX
Exec=${BIN_FILENAME}
Icon=${APP}
Terminal=false
Type=Application
Categories=Office;Finance;
EOF
chmod 644 "${DOT_DESKTOP_BUILDPATH}"

# Build the installation archive (.deb file).
dpkg-deb --build --root-owner-group "${DEB_DIR}"
