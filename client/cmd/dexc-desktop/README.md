Make sure to update the version in **client/app/VERSION** before packaging.

## Debian
Build with `./debian-package.sh`. The deb archive is located in **./build**.

For development, you'll need to install the WebKit Development Libraries.
`apt install libgtk-3-dev libwebkit2gtk-4.0-dev`
For production, they are specified as DEPENDS in the control file and the
package manager will install them.
