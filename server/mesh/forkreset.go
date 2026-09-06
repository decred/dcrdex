// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"decred.org/dcrdex/server/db"
)

// forkResetHashPrefixLen is the number of tip hash bytes in a fork reset
// token.
const forkResetHashPrefixLen = 8

// forkResetToken returns the --meshforkreset token for the frontier, in the
// form "seq:tiphash-prefix". It returns "" if the event log is empty. A node
// that halts on a fork puts this token in its halt error.
func forkResetToken(p *db.EventLogPosition) string {
	if p == nil || p.Seq == 0 || len(p.TipHash) < forkResetHashPrefixLen {
		return ""
	}
	return fmt.Sprintf("%d:%x", p.Seq, p.TipHash[:forkResetHashPrefixLen])
}

// ValidateForkResetToken checks the operator's --meshforkreset token against
// the current frontier. It returns nil if the token matches, and the wipe can
// proceed.
func ValidateForkResetToken(token string, frontier *db.EventLogPosition) error {
	if frontier == nil || frontier.Seq == 0 {
		return fmt.Errorf("the event log is empty, there is nothing to reset")
	}
	seqStr, hashStr, found := strings.Cut(strings.TrimSpace(token), ":")
	if !found {
		return fmt.Errorf("malformed token %q: expected <seq>:<tiphash-prefix>", token)
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil || seq == 0 {
		return fmt.Errorf("malformed token sequence %q", seqStr)
	}
	prefix, err := hex.DecodeString(hashStr)
	if err != nil || len(prefix) < forkResetHashPrefixLen {
		return fmt.Errorf("malformed token tip-hash prefix %q: expected %d hex bytes",
			hashStr, forkResetHashPrefixLen)
	}
	if seq != frontier.Seq || !bytes.HasPrefix(frontier.TipHash, prefix) {
		return fmt.Errorf("token %q does not match the current frontier %s", token, frontier)
	}
	return nil
}
