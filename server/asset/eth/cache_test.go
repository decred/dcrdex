//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestPrime(t *testing.T) {
	tests := []struct {
		name       string
		bestHdr    *types.Header
		bestHdrErr error
		wantErr    bool
	}{{
		name:    "ok",
		bestHdr: &types.Header{Number: big.NewInt(0)},
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}}

	for _, test := range tests {
		hc := newHashCache(tLogger)
		node := &testNode{
			bestHdr:    test.bestHdr,
			bestHdrErr: test.bestHdrErr,
		}
		err := hc.prime(nil, node)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
	}
}

func TestPoll(t *testing.T) {
	blkHdr := &types.Header{Number: big.NewInt(0)}
	tests := []struct {
		name                       string
		bestHdr, hdrByHeight       *types.Header
		bestHdrErr, hdrByHeightErr error
		wantErr, preventSend       bool
	}{{
		name:    "ok nothing to do",
		bestHdr: blkHdr,
	}, {
		name: "ok sequencial",
		bestHdr: &types.Header{
			Number:     big.NewInt(1),
			ParentHash: blkHdr.Hash(),
		},
	}, {
		name: "ok fast blocks",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
		hdrByHeight: blkHdr,
	}, {
		name: "ok reorg",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
	}, {
		name: "ok but cannot send",
		bestHdr: &types.Header{
			Number:     big.NewInt(1),
			ParentHash: blkHdr.Hash(),
		},
		preventSend: true,
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}, {
		name: "header by height error",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
		hdrByHeightErr: errors.New(""),
		wantErr:        true,
	}}

	for _, test := range tests {
		hc := newHashCache(tLogger)
		node := &testNode{
			bestHdr:        new(types.Header),
			hdrByHeight:    test.hdrByHeight,
			hdrByHeightErr: test.hdrByHeightErr,
		}
		*node.bestHdr = *blkHdr
		err := hc.prime(nil, node)
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if test.bestHdr != nil {
			*node.bestHdr = *test.bestHdr
		}
		node.bestHdrErr = test.bestHdrErr
		chSize := 1
		if test.preventSend {
			chSize = 0
		}
		ch := hc.blockChannel(chSize)
		bu := new(asset.BlockUpdate)
		wait := make(chan struct{})
		go func() {
			if test.preventSend {
				close(wait)
				return
			}
			select {
			case bu = <-ch:
			case <-time.After(time.Second * 2):
			}
			close(wait)
		}()
		hc.poll(nil)
		<-wait
		if test.wantErr {
			if bu.Err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if bu.Err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, bu.Err)
		}
	}
}
