# Build Instructions for bisonw-desktop

bison-desktop is a cross-platform desktop application for Bison Wallet. The instructions below are for building the client from source. If you are looking for pre-built binaries, see the [releases page](https://github.com/decred/dcrdex/releases).

**Note**: The instructions below are to be run in your CLI from the `bisonw-desktop` directory.

## Debian

### Install build dependencies

```bash
sudo apt-get install libgtk-3-dev libwebkit2gtk-4.1-dev build-essential
```

### Run build

```bash
./pkg/pkg-debian.sh
```

The deb archive will be located in **./build**.

## Snap

Snap is a package standard that can be installed on many Linux distributions from the Snap Store, and via software centers in e.g. Fedora, Ubuntu.

### Preparing the build environment

```bash
sudo apt-get install snapd
sudo snap install --classic snapcraft
sudo adduser $USER lxd
newgrp lxd
lxd init --auto
sudo ufw disable # if ufw is installed on the host, this is neccessary for lxd to have network access

```

### Running the build

 1. Build the Debian package
 2. `./pkg/pkg-snap.sh`

### Publishing the snap

The snap can be uploaded to the Snap Store using `./pkg/publish-snap.sh`.  This requires [Snapcraft developer account credentials](https://snapcraft.io/docs/releasing-your-app).  After this is completed, the package can be installed on any system running `snap` by running `snap install bisonw`.  The app will be available on the [Snap Store](https://snapcraft.io/store/bisonw).

## macOS (darwin)

Build with `./pkg/pkg-darwin.sh` for `amd64` MacOS machines. If running on an `arm64` machine, you'll need to build with `TARGETS=darwin/arm64 ./pkg/pkg-darwin.sh` instead. The `.dmg` click installer can be located in **./pkg/installers** after a successful build.
