// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:generate go run github.com/tc-hib/go-winres@v0.3.0 make --in winres.json --arch "386,amd64"

package main

// After generating the rsrc_windows_amd64.syso file, it will be included in the
// binary when making an amd64 build for windows:
//
//  GOOS=windows GOARCH=amd64 go build -v
