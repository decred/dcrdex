# Build Instructions for bisonw-desktop

bison-desktop is a cross-platform desktop application for Bison Wallet. The instructions below are for building the client from source. If you are looking for pre-built binaries, see the [releases page](https://github.com/decred/dcrdex/releases).

**Note**: The instructions below are to be run in your CLI from the `bisonw-desktop` directory.

## Debian
Build with `./pkg/pkg-debian.sh`. The deb archive is located in **./build**.

For development, you'll need to install the WebKit Development Libraries.
`apt install libgtk-3-dev libwebkit2gtk-4.1-dev`
For production, they are specified as DEPENDS in the control file and the
package manager will install them.

## MacOS (darwin)
Build with `./pkg/pkg-darwin.sh` for `amd64` MacOS machines. If running on an
`arm64` machine, you'll need to build with `TARGETS=darwin/arm64 ./pkg/pkg-darwin.sh` instead.
The `.dmg` click installer can be located in **./pkg/installers** after a successful build.
