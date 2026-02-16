// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/decred/politeia/politeiad/plugins/usermd"
	piv1 "github.com/decred/politeia/politeiawww/api/pi/v1"
	rv1 "github.com/decred/politeia/politeiawww/api/records/v1"
)

// userMetadataDecode returns the parsed data for the usermd plugin metadata
// stream.
func userMetadataDecode(ms []rv1.MetadataStream) (*usermd.UserMetadata, error) {
	for _, m := range ms {
		if m.PluginID != usermd.PluginID || m.StreamID != usermd.StreamIDUserMetadata {
			continue // This is not user metadata.
		}
		var um usermd.UserMetadata
		err := json.Unmarshal([]byte(m.Payload), &um)
		if err != nil {
			return nil, err
		}
		return &um, nil
	}
	return nil, fmt.Errorf("user metadata not found")
}

// proposalMetadataDecode returns the parsed data for the pi plugin proposal
// metadata stream.
func proposalMetadataDecode(fs []rv1.File) (*piv1.ProposalMetadata, string, error) {
	var proposalDesc string
	var pm *piv1.ProposalMetadata
	for _, f := range fs {
		if f.Name == piv1.FileNameIndexFile {
			b, err := base64.StdEncoding.DecodeString(f.Payload)
			if err != nil {
				return nil, "", err
			}

			proposalDesc = string(b)
			continue
		}

		if f.Name == piv1.FileNameProposalMetadata {
			b, err := base64.StdEncoding.DecodeString(f.Payload)
			if err != nil {
				return nil, "", err
			}

			err = json.Unmarshal(b, &pm)
			if err != nil {
				return nil, "", err
			}
			continue
		}

		if proposalDesc != "" && pm != nil {
			break
		}
	}

	if proposalDesc == "" {
		return nil, "", errors.New("proposal description not found")
	}

	if pm == nil {
		return nil, "", errors.New("proposal metadata not found")
	}

	return pm, proposalDesc, nil
}

// statusTimestamps contains the published, censored and abandoned timestamps
// that maybe used on Bison Wallet UI.
type statusTimestamps struct {
	published int64
	censored  int64
	abandoned int64
}

// statusChangeMetadataDecode returns the published, censored and abandoned
// dates from status change metadata streams in the statusTimestamps struct.
// It also returns the status change message for the latest metadata stream.
func statusChangeMetadataDecode(md []rv1.MetadataStream) (*statusTimestamps, string, error) {
	var statuses []usermd.StatusChangeMetadata
	for _, v := range md {
		if v.PluginID != usermd.PluginID || v.StreamID != usermd.StreamIDStatusChanges {
			continue // This is not status change metadata.
		}

		// The metadata payload is a stream of encoded json objects.
		// Decode the payload accordingly.
		d := json.NewDecoder(strings.NewReader(v.Payload))
		for {
			var sc usermd.StatusChangeMetadata
			err := d.Decode(&sc)
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, "", err
			}
			statuses = append(statuses, sc)
		}
	}

	// Status change metadata represents the data associated with each status
	// change a proposal undergoes. A proposal can only ever be on one status
	// once. Therefore, walk the statuses metadatas and parse the data we need
	// from the public, censored and abandoned status, as well as the status
	// change message from the latest one.
	var (
		timestamps         statusTimestamps
		changeMsg          string
		changeMsgTimestamp int64
	)
	for _, v := range statuses {
		if v.Timestamp > changeMsgTimestamp {
			changeMsg = v.Reason
			changeMsgTimestamp = v.Timestamp
		}
		switch rv1.RecordStatusT(v.Status) {
		case rv1.RecordStatusPublic:
			timestamps.published = v.Timestamp
		case rv1.RecordStatusCensored:
			timestamps.censored = v.Timestamp
		case rv1.RecordStatusArchived:
			timestamps.abandoned = v.Timestamp
		}
	}

	return &timestamps, changeMsg, nil
}
