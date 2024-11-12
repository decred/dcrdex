#!/usr/bin/env bash

# A good getting-started guide for Debian packaging can be found at
# https://www.internalpointers.com/post/build-binary-deb-package-practical-guide

set -ex

APP="bisonw"
VER="1.0.2"
META="release"
REV="0"
ARCH="amd64"

# DEB_NAME follows the prescribed format for debian packaging.
DEB_NAME="${APP}_${VER}-${REV}_${ARCH}"

# The build directory will be deleted at the beginning of every build. The
# directory is .gitignore'd.
BUILD_DIR="./build"

# A directory for binary source files e.g. image files.
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

# The bisonw binary.
BIN_TARGETDIR="/usr/lib/bisonw"
BIN_BUILDDIR="${DEB_DIR}${BIN_TARGETDIR}"
BIN_FILENAME="${APP}"
BIN_BUILDPATH="${BIN_BUILDDIR}/${BIN_FILENAME}"

ICON_FILENAME="bisonw.png"
SRC_TARGETDIR="${BIN_TARGETDIR}/src"
SRC_BUILDDIR="${DEB_DIR}${SRC_TARGETDIR}"
LIBICON_BUILDPATH="${SRC_BUILDDIR}/${ICON_FILENAME}"

# The Desktop Entry is a format for "installing" programs on Linux, creating
# an entry in the main menu.
# https://specifications.freedesktop.org/desktop-entry-spec/latest/
DOT_DESKTOP_TARGETDIR="/usr/share/applications"
DOT_DESKTOP_BUILDDIR="${DEB_DIR}${DOT_DESKTOP_TARGETDIR}"
DOT_DESKTOP_FILENAME="bisonw.desktop"
DOT_DESKTOP_BUILDPATH="${DOT_DESKTOP_BUILDDIR}/${DOT_DESKTOP_FILENAME}"

# This will be the icon shown for the program in the taskbar. I know that both
# PNG and SVG will work. If it's a bitmap, should probably be >= 128 x 128 px.
ICON_TARGETDIR="/usr/share/pixmaps"
ICON_BUILDDIR="${DEB_DIR}${ICON_TARGETDIR}"
DESKTOPICON_BUILDPATH="${ICON_BUILDDIR}/${ICON_FILENAME}"

# Prepare the directory structure.
rm -fr "${BUILD_DIR}"
mkdir -p -m 0755 "${CONTROL_DIR}"
mkdir -p "${SRC_BUILDDIR}" # subdir of BIN_BUILDDIR
mkdir -p "${DOT_DESKTOP_BUILDDIR}"
mkdir -p "${ICON_BUILDDIR}"

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
Depends: libgtk-3-0, libwebkit2gtk-4.0-37
Description: A multi-wallet backed by Decred DEX
EOF

# Symlink the binary and update the desktop icons, refresh the "start" menu.
cat > "${POSTINST_PATH}" <<EOF
ln -s "${BIN_TARGETDIR}/${BIN_FILENAME}" "/usr/bin/${BIN_FILENAME}"
update-desktop-database
EOF
chmod 775 "${POSTINST_PATH}"

# Remove symlink from postinst.
cat > "${POSTRM_PATH}" <<EOF
rm "/usr/bin/${BIN_FILENAME}"
EOF
chmod 775 "${POSTRM_PATH}"

# Example file:
# https://specifications.freedesktop.org/desktop-entry-spec/latest/apa.html
cat > "${DOT_DESKTOP_BUILDPATH}" <<EOF
[Desktop Entry]
Version=${VER}
Name=Bison Wallet
Comment=Multi-wallet backed by Decred DEX
Exec=${BIN_TARGETDIR}/${BIN_FILENAME}
Icon=${ICON_TARGETDIR}/${ICON_FILENAME}
Terminal=false
Type=Application
Categories=Office;Development;
EOF
chmod 644 "${DOT_DESKTOP_BUILDPATH}"

# Icon
cp "${SRC_DIR}/${ICON_FILENAME}" "${DESKTOPICON_BUILDPATH}"
chmod 644 "${DESKTOPICON_BUILDPATH}"

cp "${SRC_DIR}/${ICON_FILENAME}" "${LIBICON_BUILDPATH}"
chmod 644 "${LIBICON_BUILDPATH}"

# Build the installation archive (.deb file).
dpkg-deb --build --root-owner-group "${DEB_DIR}"
