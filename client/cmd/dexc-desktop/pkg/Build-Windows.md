## Windows build

### Setting up the build environment

Download and run setup as Administrator (privileges are required in order to set environment variables).  Open a command prompt as Administrator, and run:

 ```batch
 curl -O https://raw.githubusercontent.com/decred/dcrdex/windows-build/client/cmd/dexc-desktop/pkg/setup-windows.cmd
 setup-windows.cmd <branch> <repoUrl>
 ```

 Both `branch` and `repoUrl` are optional.  If not specified, it will clone the `master` branch in the default [dcrdex repository](https://github.com/decred/dcrdex).

 This will download and install `git` and `PowerShell`, clone the repo and install the build toolchain and required SDKs for the build.  Once completed, close the above command prompt, open a new prompt in order to effectuate `PATH` and other environment variables configured by the setup script.  Administrator privileges are NOT required.

 ### Running the build

 ```batch
 cd dcrdex\client\cmd\dexc-desktop
 ```

Build the Windows binary: 

```batch
pkg\build-windows.cmd
```

The resulting `.exe` will be in `build/`.  This will also build the site bundle if `client/webserver/site/dist` does not exist.

### Build the MSI (Windows Installer)

```batch
pkg\pkg-windows.cmd
```

The resulting installer binary will be located in `build\msi`.
