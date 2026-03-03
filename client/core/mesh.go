package core

import (
	"context"

	"decred.org/dcrdex/dex"
)

func (c *Core) coreMesh() *Mesh {
	c.meshMtx.RLock()
	mesh, meshCM := c.mesh, c.meshCM
	c.meshMtx.RUnlock()
	if mesh == nil || !meshCM.On() {
		return nil
	}
	return &Mesh{}

}

func (c *Core) connectMesh(ctx context.Context) {
	if c.net != dex.Simnet || !c.cfg.Mesh {
		return
	}
	c.meshMtx.RLock()
	mesh, meshCM := c.mesh, c.meshCM
	c.meshMtx.RUnlock()
	var err error
	if err = meshCM.ConnectOnce(c.ctx); err != nil {
		c.log.Errorf("error connecting mesh: %v", err)
		return
	}
	defer func() {
		if err != nil {
			c.log.Error("Failed to initialize mesh subscriptions. Closing connection: %v", err)
			meshCM.Disconnect()
		}
	}()

	c.log.Infof("Connected to Mesh")

	if err = mesh.SubscribeToFeeRateEstimates(ctx); err != nil {
		c.log.Errorf("error subscribing to mesh fee rate estimates: %v", err)
		return
	}
	if err = mesh.SubscribeToTickerPrices(ctx); err != nil {
		c.log.Error("error subscribing to mesh fiat rates: %v", err)
		return
	}
}
