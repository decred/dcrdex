//go:build !xmr

// Non-xmr build: the xmr package is not compiled in, so the XMR
// asset adapter cannot be constructed. Returns nil; the adaptor
// orchestrator will fail cleanly on the first XMR call.

package core

import (
	"context"

	"decred.org/dcrdex/client/core/adaptorswap"
)

func buildAdaptorXMRAdapter(ctx context.Context, w *xcWallet) adaptorswap.XMRAssetAdapter {
	return nil
}
