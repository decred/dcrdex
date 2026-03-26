# Windows build

## Native build

### Setting up the build environment

This build setup is expected to be run on a freshly installed system (e.g. in a VM), which is the best practice for building binaries for release on Windows, therefore there are no prerequisites.  The setup script will install all components of the toolchain, dependencies and SDKs required to build the `bisonw-desktop` binary and the MSI (MS installer), there is no need to install anything manually before running this (it can actually cause issues if different versions of the dependencies are already on the system).  The builder was tested on Windows 10, Windows 11 and server equivalents, 2019/2022.

Download and run setup as Administrator (privileges are required in order to set environment variables).  Open a **Command Prompt** (cmd.exe) as Administrator (not PowerShell), and run:

 ```batch
 cd %UserProfile%
 curl -O https://raw.githubusercontent.com/decred/dcrdex/master/client/cmd/bisonw-desktop/windows/setup-windows.cmd
 setup-windows.cmd <branch> <repoUrl>
 ```

 Both `branch` and `repoUrl` are optional.  If not specified, it will clone the `master` branch in the default [dcrdex repository](https://github.com/decred/dcrdex).

 This will download and install `git` and `PowerShell`, clone the repo and install the build toolchain and required SDKs for the build.  Once completed, close the above command prompt, open a new prompt in order to effectuate `PATH` and other environment variables configured by the setup script.  Administrator privileges are NOT required for the rest of the steps.

 ### Running the build

 ```batch
 cd dcrdex\client\cmd\bisonw-desktop
 ```

Build the Windows binary:

```batch
windows\build-windows.cmd
```

This will also build the site bundle if `client/webserver/site/dist` does not exist.

The resulting `.exe` will be in `build\windows`.

### Build the MSI (Windows Installer)

```batch
windows\pkg-windows.cmd
```

The resulting installer binary will be located in `build\msi`.

## Cross-compiling from Linux with Docker

The Windows `.exe` can be cross-compiled from Linux using Docker. This does not produce the MSI installer.

### Prerequisites

Build the site bundle if it hasn't been built already:

```bash
cd client/webserver/site
npm clean-install
npm run build
cd ../../..
```

### Build the exe

From the repo root:

```bash
docker build -t bisonw-desktop-windows -f client/cmd/bisonw-desktop/windows/Dockerfile .
```

To include XMR support:

```bash
docker build -t bisonw-desktop-windows --build-arg XMR=1 -f client/cmd/bisonw-desktop/windows/Dockerfile .
```

### Extract the build output

```bash
mkdir -p build && docker run --rm bisonw-desktop-windows tar -cf - -C /out . | tar -xf - -C ./build
```

The `build/` directory will contain `bisonw-desktop.exe`, `WebView2Loader.dll`, `libwinpthread-1.dll`, and `libwallet2_api_c.dll` if XMR was enabled.

### Packaging the MSI on Windows

The MSI installer can only be built on Windows. If you cross-compiled the exe on Linux, copy the contents of `build/` to `client\cmd\bisonw-desktop\build\windows\` in the repo on a Windows machine with the build environment set up, then run from `client\cmd\bisonw-desktop`:

```batch
windows\pkg-windows.cmd --skip-build
```

To include XMR support in the MSI:

```batch
windows\pkg-windows.cmd --skip-build --xmr
```

The resulting installer will be in `build\msi`.

**Note:** The Docker cross-compilation uses MinGW, which produces a binary that depends on `libwinpthread-1.dll`. This DLL is bundled automatically by the Dockerfile. Native Windows builds use MSVC and do not require this DLL.
