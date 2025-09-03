package core

import (
	"fmt"

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
	mesh := c.mesh
	meshCM := c.meshCM
	c.meshMtx.RUnlock()
	if mesh == nil || !meshCM.On() {
		return nil
	}
	return &Mesh{}

}
func (c *Core) connectMesh() {
	fmt.Println("--connectMesh.0")
	if c.net != dex.Simnet {
		return
	}
	var err error
	if err = c.meshCM.ConnectOnce(c.ctx); err != nil {
		c.log.Errorf("error connecting mesh: %v", err)
		return
	}
	fmt.Println("--connectMesh.1")
	defer func() {
		if err != nil {
			c.log.Errorf("Failed to initialize mesh subscriptions. Closing connection:", err)
			c.meshCM.Disconnect()
		}
	}()

	c.log.Infof("Connected to Mesh")

	if err = c.mesh.SubscribeToFeeRateEstimates(); err != nil {
		c.log.Errorf("error subscribing to mesh fee rate estimates: %v", err)
		return
	}
	fmt.Println("--connectMesh.2")
	if err := c.mesh.SubscribeToFiatRates(); err != nil {
		c.log.Error("error subscribing to mesh fiat rates: %v", err)
		return
	}
	fmt.Println("--connectMesh.3")
}

func deriveMeshPriv(seed []byte) (*secp256k1.PrivateKey, error) {
	xKey, err := keygen.GenDeepChild(seed, []uint32{hdKeyPurposeBonds})
	if err != nil {
		return nil, err
	}
	privB, err := xKey.SerializedPrivKey()
	if err != nil {
		return nil, err
	}
	return secp256k1.PrivKeyFromBytes(privB), nil
}
