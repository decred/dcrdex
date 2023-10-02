# Build Instructions for dexc-desktop

The dexc-desktop client is a cross-platform desktop application for the DEX client and wallet. The instructions below are for building the client from source. If you are looking for pre-built binaries, see the [releases page](https://github.com/decred/dcrdex/releases).

**Note**: The instructions below are to be run in your CLI from the `dexc-desktop` directory.

## Debian
Build with `./pkg/pkg-debian.sh`. The deb archive is located in **./build**.

For development, you'll need to install the WebKit Development Libraries and the standard build toolchain:

```bash
apt install libgtk-3-dev libwebkit2gtk-4.0-dev build-essential
```
For production, they are specified as DEPENDS in the control file and the package manager will install them.

## Snap

This build environment uses LXD containers under the hood.   It's advised to run this on an Ubuntu system (ideally in a VM; the Snap components require system-wide installation).  Ubuntu takes care of the configuration of the toolchain, other distributions require additional steps to initialize LXD.

Build environment setup:

```bash
sudo apt-get install snapd
sudo snap install --classic snapcraft
```

Run the build:

```bash
./pkg/pkg-snap.sh
```

This builds the snap package in **./build**.  Note that this is dependent on the Debian package being in place so be sure to run `./pkg/pkg-debian.sh` first.

### Testing locally

```bash
sudo snap install --dangerous build/dexc_*.snap
```

### Publishing to the Snap store

The snap can be uploaded to the Snap Store using `./pkg/publish-snap.sh`. Once the publish is complete the package can be installed on other systems using `snap install dexc`.

## MacOS (darwin)
Build with `./pkg/pkg-darwin.sh` for `amd64` MacOS machines. If running on an `arm64` machine, you'll need to build with `TARGETS=darwin/arm64 ./pkg/pkg-darwin.sh` instead.  The `.dmg` click installer can be located in **./pkg/installers** after a successful build.
