package core

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/tatanka/mj"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func (c *Core) handleMeshNotification(noteI any) {
	switch n := noteI.(type) {
	case *mj.Broadcast:
		c.handleMeshBroadcast(n)
	}
}

func (c *Core) handleMeshBroadcast(bcast *mj.Broadcast) {
	c.log.Tracef("Received mesh broadcast: %s", bcast.Subject)

	switch bcast.Topic {
	case mj.TopicMarket:
		// c.handleMarketBroadcast(bcast)
	case mj.TopicFeeRateEstimate:
		// c.handleFeeRateEstimateBroadcast(bcast)
	case mj.TopicFiatRate:
		// c.handleFiatRateBroadcast(bcast)
	default:
		c.log.Warnf("Unknown broadcast topic: %s", bcast.Topic)
	}

	// // Emit the broadcast to the subscribers.
	// c.meshEmit(bcast)
}

func (c *Core) coreMesh() *Mesh {
	c.meshMtx.RLock()
	mesh, meshCM := c.mesh, c.meshCM
	c.meshMtx.RUnlock()
	if mesh == nil || !meshCM.On() {
		return nil
	}
	return &Mesh{}

}

func (c *Core) connectMesh() {
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

	if err = mesh.SubscribeToFeeRateEstimates(); err != nil {
		c.log.Errorf("error subscribing to mesh fee rate estimates: %v", err)
		return
	}
	if err = mesh.SubscribeToFiatRates(); err != nil {
		c.log.Error("error subscribing to mesh fiat rates: %v", err)
		return
	}
}

func deriveMeshPriv(seed []byte) (*secp256k1.PrivateKey, error) {
	xKey, err := keygen.GenDeepChild(seed, []uint32{hdKeyPurposeMesh})
	if err != nil {
		return nil, err
	}
	privB, err := xKey.SerializedPrivKey()
	if err != nil {
		return nil, err
	}
	return secp256k1.PrivKeyFromBytes(privB), nil
}
