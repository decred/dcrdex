#!/usr/bin/env bash

# Bail out on any unhandled errors
set -e;

# Any command that exits with non-zero code will cause the pipeline to fail
set -o pipefail;

export GOWORK=off

# For release set metadata to "release".
VER="1.1.0-pre"
META= # "release"
BUILD_VER="1.1.0" # increment for every build.
OS_FULL_VERSION="$(sw_vers | sed -n 2p | cut -d : -f 2 | tr -d '[:space:]' | cut -c1-)"
OS_MAJOR_VERSION="$(echo $OS_FULL_VERSION | cut -d . -f 1)"
OS_MINOR_VERSION="$(echo $OS_FULL_VERSION | cut -d . -f 2)"

# if META set, append "+${META}", otherwise nothing.
LDFLAGS_BASE="-buildid= -s -w -X main.Version=${VER}${META:++${META}}"

# App information
APP_NAME="Bison Wallet"
VOLUME_NAME="Bison Wallet ${VER}${META:++${META}}"

# Filepaths to important directories.
SCRIPTPATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
SRC_DIR="${SCRIPTPATH}/../src"
INSTALLERS_DIR="$SCRIPTPATH/installers"
APP_DIR="${SCRIPTPATH}/${APP_NAME}.app"
CONTENTS_DIR="${APP_DIR}/Contents"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"
APP_EXCE_DIR="${CONTENTS_DIR}/MacOS"

# For fancy disk image container icon and background.
ICON_FILE_NAME=bisonw.icns
ICON_FILE="${SRC_DIR}/bisonw.icns"
VOLUME_ICON_FILE="${ICON_FILE}" # use the same icon for the volume.
BACKGROUND_FILE="${SRC_DIR}/bisonw-installer-bg.tiff"

function cleanup() {
	echo "Removing build files..."
	rm -rf "$APP_DIR"
}

export MACOSX_DEPLOYMENT_TARGET=10.11.0

function prepare() {
	# Remove the installers directories and recreate them.
	rm -rf "${INSTALLERS_DIR}"
	mkdir -p "${INSTALLERS_DIR}"

	# Create .app and resource directory directory.
	rm -rf "${RESOURCES_DIR}"
	mkdir -p "${RESOURCES_DIR}"

	# Copy icon to the resource directory.
	cp "${ICON_FILE}" "${RESOURCES_DIR}"

	# Add the Info.plist file that holds basic information about the app.
	echo  "<?xml version="1.0" encoding="UTF-8"?>
		  <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
		  <plist version="1.0"><dict>
		  <key>CFBundleDevelopmentRegion</key><string>en</string>
		  <key>CFBundleDisplayName</key><string>${APP_NAME}</string>
		  <key>CFBundleExecutable</key><string>${APP_NAME}</string>
		  <key>CFBundleIconFile</key><string>${ICON_FILE_NAME}</string>
		  <key>CFBundleIdentifier</key><string>com.decred.dcrdex</string>
		  <key>CFBundleInfoDictionaryVersion</key><string>1.0</string>
		  <key>CFBundleName</key><string>${APP_NAME}</string>
		  <key>CFBundleShortVersionString</key><string>${VER%%-*}</string>
		  <key>CFBundleVersion</key><string>${BUILD_VER}</string>
		  <key>CFBundleSignature</key><string>dexc</string>
		  <key>CFBundleSupportedPlatforms</key><array><string>MacOSX</string></array>
		  <key>LSMinimumSystemVersion</key><string>${MACOSX_DEPLOYMENT_TARGET}</string>
		  <key>NSHighResolutionCapable</key><true/>
		  <key>NSRequiresAquaSystemAppearance</key><false/>
		  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>
		  <key>CFBundlePackageType</key><string>APPL</string>
		  <key>NSUserNotificationAlertStyle</key><string>alert</string>
		  </dict></plist>" > "${CONTENTS_DIR}/Info.plist"
}

# Build the webpack bundle prior to building the webserver package, which embeds
# the files. Don't do this for release builds because the assets are committed.
if [ "${META}" != "release" ]; then
	pushd ${SCRIPTPATH}/../../../webserver/site
	go generate # just check, no write
	npm ci
	npm run build
	popd
fi

function build_targets() {
  for TARGET in ${TARGETS}; do
    OS=${TARGET%%/*}
    ARCH=${TARGET##*/}

    echo "Building .DMG click installer for ${OS}-${ARCH}"

	TARGET_NAME="bisonw-desktop-${OS}-${ARCH}-v${VER}"

	# Remove any existing executable if any.
	rm -rf "${APP_EXCE_DIR}"
	mkdir -p "${APP_EXCE_DIR}"

	# Potentially want to run with CGO_CXXFLAGS="-mmacosx-version-min=10.11.0"

    pushd "${SCRIPTPATH}/.."
    GOOS=${OS} GOARCH=${ARCH} CGO_ENABLED=1 go build -v -trimpath -o "${APP_EXCE_DIR}/${APP_NAME}" -ldflags "${LDFLAGS_DEXC:-${LDFLAGS_BASE}}"
    popd

	pushd "${SCRIPTPATH}"
	./create-dmg.sh \
		--volname "${VOLUME_NAME}" \
		--volicon "${VOLUME_ICON_FILE}" \
		--background "${BACKGROUND_FILE}" \
		--window-pos 100 100 \
		--window-size 550 360 \
		--icon-size 60 \
		--text-size 12 \
		--icon "${APP_NAME}" 150 210 \
		--app-drop-link 380 210 \
		"${INSTALLERS_DIR}/${TARGET_NAME}.dmg" \
		"${APP_DIR}"
	popd

  done
}

TARGETS=${TARGETS:-"darwin/arm64"}
prepare
build_targets
cleanup

pushd "${INSTALLERS_DIR}"
shasum -a 256 *.dmg > dexc-v${VER}-manifest.txt
popd
