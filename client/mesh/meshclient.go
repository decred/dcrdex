// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/bisoncraft/mesh/bond"
	mc "github.com/bisoncraft/mesh/client"
	"github.com/bisoncraft/mesh/protocols"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/hdkeychain/v3"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
)

const hdKeyPurposeMesh uint32 = hdkeychain.HardenedKeyStart + 0x6d657368 // ASCII "mesh"

type Client struct {
	*mc.Client
	log          dex.Logger
	ch           chan any
	netTickerMap map[string]uint32
}

type ClientConfig struct {
	Seed          []byte
	BootstrapPeer string
	Bonds         []*bond.BondParams
	Log           dex.Logger
}

func NewClient(cfg *ClientConfig) (*Client, error) {
	defer encode.ClearBytes(cfg.Seed)

	meshPriv, err := deriveMeshPriv(cfg.Seed)
	if err != nil {
		return nil, fmt.Errorf("error deriving mesh private key: %w", err)
	}
	cl, err := mc.NewClient(&mc.Config{
		PrivateKey:      meshPriv,
		RemotePeerAddrs: []string{cfg.BootstrapPeer},
		Bonds:           cfg.Bonds,
		Logger:          cfg.Log.SubLogger("mc"),
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		Client:       cl,
		log:          cfg.Log,
		ch:           make(chan interface{}, 128),
		netTickerMap: asset.NetworkTickers(),
	}, nil
}

func (c *Client) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.Client.Run(ctx)
		wg.Done()
	}()
	return &wg, nil
}

type TickerPriceUpdate struct {
	Ticker string
	Price  float64
}

func (c *Client) SubscribeToTickerPrices(ctx context.Context) error {
	tickers := asset.Tickers()
	topics := make([]string, len(tickers))
	for i, ticker := range tickers {
		topics[i] = protocols.PriceTopicPrefix + ":" + ticker
	}
	return c.Client.Subscribe(ctx, topics, func(topic string, ev mc.TopicEvent) {
		ticker, ok := extractSubtopic(topic, protocols.PriceTopicPrefix)
		if !ok {
			c.log.Warnf("Invalid price topic format: %s", topic)
			return
		}
		c.emit(ctx, &TickerPriceUpdate{
			Ticker: ticker,
			Price:  decodeFloat64(ev.Data),
		})
	})
}

type FeeRateUpdate struct {
	NetAssetID uint32
	FeeRate    uint64
}

func (c *Client) SubscribeToFeeRateEstimates(ctx context.Context) error {
	netTickers := make([]string, 0, 10)
	for _, a := range asset.Assets() {
		// Base Network assets don't have a "." in their symbol.
		if len(strings.Split(a.Symbol, ".")) != 1 {
			continue
		}
		netTickers = append(netTickers, a.Info.UnitInfo.Conventional.Unit)
	}
	topics := make([]string, len(netTickers))
	for i, net := range netTickers {
		topics[i] = protocols.FeeRateTopicPrefix + ":" + net
	}
	return c.Client.Subscribe(ctx, topics, func(topic string, ev mc.TopicEvent) {
		netTicker, ok := extractSubtopic(topic, protocols.FeeRateTopicPrefix)
		if !ok {
			c.log.Warnf("Invalid price topic format: %s", topic)
			return
		}
		netAssetID, ok := c.netTickerMap[netTicker]
		if !ok {
			c.log.Warnf("Unknown network identifier %q", netTicker)
			return
		}
		c.emit(ctx, &FeeRateUpdate{
			NetAssetID: netAssetID,
			FeeRate:    bytesToBigInt(ev.Data).Uint64(),
		})
	})
}

func (c *Client) emit(ctx context.Context, thing any) {
	select {
	case c.ch <- thing:
	case <-time.After(time.Second * 10):
		c.log.Errorf("Mesh event channel blocking for > 10 seconds: %v", thing)
	case <-ctx.Done():
	}
}

func extractSubtopic(topic, namespace string) (subtopic string, ok bool) {
	parts := strings.SplitN(topic, ":", 2)
	if len(parts) != 2 || parts[0] != namespace {
		return "", false
	}
	return parts[1], true
}

func decodeFloat64(buf []byte) float64 {
	bits := binary.BigEndian.Uint64(buf)
	return math.Float64frombits(bits)
}

func bytesToBigInt(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

func deriveMeshPriv(seed []byte) (libp2pcrypto.PrivKey, error) {
	entropy := blake256.Sum256(append(seed, encode.Uint32Bytes(hdKeyPurposeMesh)...))
	priv, _, err := libp2pcrypto.GenerateEd25519Key(bytes.NewReader(entropy[:]))
	return priv, err
}
