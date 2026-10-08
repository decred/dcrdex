// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"sort"

	"decred.org/dcrdex/server/market"
	"decred.org/dcrdex/server/mesh"
)

// newMasterWorkers registers the swapper before the markets so it can handle
// matches as markets start. Inactivity checks are enabled only after all
// markets are ready, so startup delays do not count against clients.
func newMasterWorkers(swapperRun func(context.Context, func(error)), enableInactionChecks func(),
	markets map[string]*market.Market) []mesh.MasterWorker {

	marketNames := make([]string, 0, len(markets))
	for name := range markets {
		marketNames = append(marketNames, name)
	}
	sort.Strings(marketNames)

	masterWorkers := make([]mesh.MasterWorker, 0, len(markets)+2)
	masterWorkers = append(masterWorkers, mesh.MasterWorker{Name: "Swapper", Run: swapperRun})
	for _, name := range marketNames {
		masterWorkers = append(masterWorkers, mesh.MasterWorker{
			Name: marketSubSysName(name),
			Run:  markets[name].Run,
		})
	}
	masterWorkers = append(masterWorkers, mesh.MasterWorker{
		Name: "Swap inactivity checks",
		Run: func(ctx context.Context, reportReady func(error)) {
			enableInactionChecks()
			reportReady(nil)
			<-ctx.Done()
		},
	})
	return masterWorkers
}
