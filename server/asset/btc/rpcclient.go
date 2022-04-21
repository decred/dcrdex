// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
)

const (
	methodGetBestBlockHash  = "getbestblockhash"
	methodGetBlockchainInfo = "getblockchaininfo"
	methodEstimateSmartFee  = "estimatesmartfee"
	methodGetTxOut          = "gettxout"
	methodGetRawTransaction = "getrawtransaction"
	methodGetBlock          = "getblock"
)

// RawRequester is for sending context-aware RPC requests, and has methods for
// shutting down the underlying connection. The returned error should be of type
// dcrjson.RPCError if non-nil.
type RawRequester interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
	Shutdown()
	WaitForShutdown()
}

// RPCClient is a bitcoind wallet RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type RPCClient struct {
	ctx       context.Context
	requester RawRequester
}

func (rc *RPCClient) callHashGetter(method string, args anylist) (*chainhash.Hash, error) {
	var txid string
	err := rc.call(method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// GetBestBlockHash returns the hash of the best block in the longest block
// chain.
func (rc *RPCClient) GetBestBlockHash() (*chainhash.Hash, error) {
	return rc.callHashGetter(methodGetBestBlockHash, nil)
}

// GetBlockchainInfoResult models the data returned from the getblockchaininfo
// command.
type GetBlockchainInfoResult struct {
	Blocks               int64  `json:"blocks"`
	Headers              int64  `json:"headers"`
	BestBlockHash        string `json:"bestblockhash"`
	InitialBlockDownload bool   `json:"initialblockdownload"`
}

// GetBlockChainInfo returns information related to the processing state of
// various chain-specific details.
func (rc *RPCClient) GetBlockChainInfo() (*GetBlockchainInfoResult, error) {
	chainInfo := new(GetBlockchainInfoResult)
	err := rc.call(methodGetBlockchainInfo, nil, chainInfo)
	if err != nil {
		return nil, err
	}
	return chainInfo, nil
}

// EstimateSmartFee requests the server to estimate a fee level.
func (rc *RPCClient) EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	res := new(btcjson.EstimateSmartFeeResult)
	return res, rc.call(methodEstimateSmartFee, anylist{confTarget, mode}, res)
}

// GetTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (rc *RPCClient) GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, rc.call(methodGetTxOut, anylist{txHash.String(), index, mempool},
		&res)
}

// GetRawTransaction retrieves tx's information.
func (rc *RPCClient) GetRawTransaction(txHash *chainhash.Hash) (*btcutil.Tx, error) {
	var txHex string
	err := rc.call(methodGetRawTransaction, anylist{txHash.String()}, &txHex)
	if err != nil {
		return nil, err
	}
	return btcutil.NewTxFromReader(hex.NewDecoder(strings.NewReader(txHex)))
}

// GetRawTransactionVerbose retrieves the verbose tx information.
func (rc *RPCClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	res := new(btcjson.TxRawResult)
	return res, rc.call(methodGetRawTransaction, anylist{txHash.String(),
		true}, res)
}

// GetBlockVerbose fetches verbose block data for the specified hash.
func (rc *RPCClient) GetBlockVerbose(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	res := new(btcjson.GetBlockVerboseResult)
	return res, rc.call(methodGetBlock, anylist{blockHash.String(), true}, res)
}

// RawRequest is a wrapper func for callers that are not context-enabled.
func (rc *RPCClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	return rc.requester.RawRequest(rc.ctx, method, params)
}

// Call is used to marshal parmeters and send requests to the RPC server via
// (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result will be
// marshaled into `thing`.
func (rc *RPCClient) Call(method string, args []interface{}, thing interface{}) error {
	return rc.call(method, args, thing)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// call is used internally to marshal parmeters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (rc *RPCClient) call(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := rc.requester.RawRequest(rc.ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}
