package firo

var (
	methodGetBlock       = "getblock"
	methodRawTransaction = "getrawtransaction"
)

type anylist []any

type firoBlkResult struct {
	Hash              string   `json:"hash"`
	Confirmations     int      `json:"confirmations"`
	Height            int      `json:"height"`
	Version           int      `json:"version"`
	VersionHex        string   `json:"versionHex"`
	Merkleroot        string   `json:"merkleroot"`
	Tx                []string `json:"tx"`
	Time              int      `json:"time"`
	MedianTime        int      `json:"mediantime"`
	Nonce             int      `json:"nonce"`
	Bits              string   `json:"bits"`
	PreviousBlockHash string   `json:"previousblockhash"`
	Chainlock         bool     `json:"chainlock"`
}

type firoTxLiteResult struct {
	Version int `json:"version"`
	Type    int `json:"type"`
}

type scriptSig struct {
	Hex string `json:"hex"`
}

type input struct {
	Txid      string    `json:"txid"`
	Vout      int       `json:"vout"`
	ScriptSig scriptSig `json:"scriptSig"`
	Value     float32   `json:"value"`
	ValueSat  int       `json:"valueSat"`
	Address   string    `json:"address"`
	Sequence  int       `json:"sequence"`
}

type scriptPubKey struct {
	Hex        string   `json:"hex"`
	ReqSigs    int      `json:"reqSigs"`
	Type       string   `json:"type"`
	Addressses []string `json:"addresses"`
}

type output struct {
	Value        float64      `json:"value"`
	N            int          `json:"n"`
	ScriptPubKey scriptPubKey `json:"scriptPubKey"`
}

type firoNormalTxResult struct {
	Txid     string   `json:"txid"`
	Hash     string   `json:"hash"`
	Size     int      `json:"size"`
	Vsize    int      `json:"vsize"`
	Version  int      `json:"version"`
	Locktime int      `json:"locktime"`
	Type     int      `json:"type"`
	Vin      []input  `json:"vin"`
	Vout     []output `json:"vout"`
}
