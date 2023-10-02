
# This file defines common variables to be source'd by the various build scripts
# in this directory.

# pick up the release tag from git
VER=$(git describe --tags --abbrev=0 --always | sed -e 's/^v//')
META= # "release"
REV="0"

APP="bisonw"
ARCH="amd64"

# The build directory will be deleted at the beginning of every build. The
# directory is .gitignore'd.
BUILD_DIR="./build"

# DEB_NAME follows the prescribed format for debian packaging.
DEB_NAME="${APP}_${VER}-${REV}_${ARCH}"

