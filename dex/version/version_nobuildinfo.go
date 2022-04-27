// Copyright (c) 2021-2022 The Decred developers
// Use of this source code is governed by an ISC license
// that can be found at https://github.com/decred/dcrd/blob/master/LICENSE.

//go:build !go1.18

package version

func vcsCommitID() string {
	return ""
}
