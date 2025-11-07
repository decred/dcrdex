<a id="top"/>

Adding support for an asset is accomplished by writing a
[Go](https://golang.org/) package with types that implement a particular set of
interfaces, defined in [common.go](https://github.com/decred/dcrdex/blob/master/server/asset/common.go)
and [driver.go](https://github.com/decred/dcrdex/blob/master/server/asset/driver.go).
There are then two ways to import the asset backend into the server software.

1. The package is compiled with -``buildmode=plugin`` and imported at runtime by specifying
the plugin in the server configuration.
2. The backend is added to the dcrdex repository, and imported directly at compile time.

With the exception of a small handful of assets which will be implemented during
initial phases of DEX development, it is expected that development communities
will release their own appropriately vetted plugins.

Existing implementations for supported assets are located
[in the server/asset directory](https://github.com/decred/dcrdex/tree/master/server/asset).

---

[⤴ Back to Top](#top)
