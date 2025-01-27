### Package libsecp256k1

Package libsecp256k1 includes some primative cryptographic functions needed for
working with adaptor signatures that are not currently found in golang. This imports
code from https://github.com/tecnovert/secp256k1 and uses that with cgo. Both
that library and this package are in an experimental stage.

### Usage

Run the `build.sh` script. Currently untested on mac and will not work on Windows.
