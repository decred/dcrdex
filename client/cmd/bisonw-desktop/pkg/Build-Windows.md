## Windows build

### Setting up the build environment

This build setup is expected to be run on a freshly installed system (e.g. in a VM), which is the best practice for building binaries for release on Windows, therefore there are no prerequisites.  The setup script will install all components of the toolchain, dependencies and SDKs required to build the `bisonw-desktop` binary and the MSI (MS installer), there is no need to install anything manually before running this (it can actually cause issues if different versions of the dependencies are already on the system).  The builder was tested on Windows 10, Windows 11 and server equivalents, 2019/2022.

Download and run setup as Administrator (privileges are required in order to set environment variables).  Open a command prompt as Administrator, and run:

 ```batch
 cd %UserProfile%
 curl -O https://raw.githubusercontent.com/decred/dcrdex/master/client/cmd/bisonw-desktop/pkg/setup-windows.cmd
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
pkg\build-windows.cmd
```

This will also build the site bundle if `client/webserver/site/dist` does not exist.

The resulting `.exe` will be in `build\windows`.

### Build the MSI (Windows Installer)

```batch
pkg\pkg-windows.cmd
```

The resulting installer binary will be located in `build\msi`.
