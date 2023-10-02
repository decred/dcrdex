# Build Instructions for dexc-desktop

The dexc-desktop client is a cross-platform desktop application for the DEX client and wallet. The instructions below are for building the client from source. If you are looking for pre-built binaries, see the [releases page](https://github.com/decred/dcrdex/releases).

**Note**: The instructions below are to be run in your CLI from the `dexc-desktop` directory.

## Debian
Build with `./pkg/pkg-debian.sh`. The deb archive is located in **./build**.

For development, you'll need to install the WebKit Development Libraries and the
standard build toolchain:

```bash
apt install libgtk-3-dev libwebkit2gtk-4.0-dev build-essential
```
For production, they are specified as DEPENDS in the control file and the
package manager will install them.

## Snap
The Snap package can be built on an Ubuntu system using Snapcraft, which can
be installed with `apt-get install snapcraft`.  Then run `./pkg/pkg-snap.sh`, 
which will build the snap package in **./build**.  Note that this is dependent on
the Debian package so be sure to run the deb build first.

The snap can be uploaded to the Snap Store using `./pkg/upload-snap.sh`, after this 
the package can be installed on any Ubuntu system using `snap install dexc`.

Prerequsities:
```bash
apt-get install snapcraft
```

## MacOS (darwin)
Build with `./pkg/pkg-darwin.sh` for `amd64` MacOS machines. If running on an
`arm64` machine, you'll need to build with `TARGETS=darwin/arm64 ./pkg/pkg-darwin.sh` instead.
The `.dmg` click installer can be located in **./pkg/installers** after a successful build.
