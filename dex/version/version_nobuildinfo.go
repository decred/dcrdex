// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2022 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build !go1.18

package version

func vcsCommitID() string {
	return ""
}
