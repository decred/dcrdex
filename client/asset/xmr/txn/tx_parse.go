package txn

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// This is a minimal parser to inspect Vin and Vout and maybe grab key images
// to freeze unspents for private swaps. So far the structs map to monero rpc.
// We do not parse RCT Signatures, CSLAG or Bulletproof plus fields.

const CurrentTransactionVersion = 2

const KeyLen = 32

const (
	TxinGen           = 0xff
	TxinToScript      = 0
	TxinToScripthash  = 1
	TxinToKey         = 2
	TxoutToScript     = 0
	TxoutToScripthash = 1
	TxoutToKey        = 2
	TxoutToTaggedKey  = 3
)

type TaggedKey struct {
	StealthKey string `json:"key"`
	ViewTag    string `json:"view_tag"`
}

type Key struct {
	StealthKey string `json:"key"`
	ViewTag    string `json:"view_tag"`
}

type Target struct {
	TaggedKey TaggedKey `json:"tagged_key"`
}

type Output struct {
	AmountCoinbase uint64 `json:"amount"`
	Target         Target `json:"target"`
}

type PubKeyPrevOut struct {
	Amount     uint64   `json:"amount"`
	KeyOffsets []uint64 `json:"key_offsets"`
	KeyImage   string   `json:"k_image"`
}

type Gen struct {
	Height uint64 `json:"height"`
}

type Input struct {
	Key      PubKeyPrevOut `json:"key"`
	Coinbase Gen           `json:"gen"`
}

type TxPrefix struct {
	Version      uint64   `json:"version"`
	TxUnlockTime uint64   `json:"unlock_time"` // unlock time for txs we lock for n blocks
	Vin          []Input  `json:"vin"`
	Vout         []Output `json:"vout"`
}

type XmrTx struct {
	Prefix     TxPrefix
	Extra      []byte
	ExtraTxKey string
	Coinbase   bool
	TxHash     uint64
}

func (x *XmrTx) GetStealthAddresses() []string {
	var stealthAddresses []string
	for _, output := range x.Prefix.Vout {
		stealthAddresses = append(stealthAddresses, output.Target.TaggedKey.StealthKey)
	}
	return stealthAddresses
}

type TxParser struct {
	rdr *bytes.Reader
	tx  *XmrTx
}

func NewTxParser(input string) (*TxParser, error) {
	b, err := hex.DecodeString(input)
	if err != nil {
		return nil, err
	}
	return &TxParser{
		rdr: bytes.NewReader(b),
		tx:  &XmrTx{},
	}, nil
}

func (p *TxParser) Parse() (*XmrTx, error) {
	err := p.readVersion()
	if err != nil {
		return nil, err
	}
	err = p.readUnlockTime()
	if err != nil {
		return nil, err
	}
	err = p.readVinVout()
	if err != nil {
		return nil, err
	}
	err = p.readExtra()
	if err != nil {
		return nil, err
	}
	return p.tx, nil
}

func (p *TxParser) readVersion() error {
	version, err := binary.ReadUvarint(p.rdr)
	if err != nil {
		return err
	}
	if version == 0 || CurrentTransactionVersion < version {
		return fmt.Errorf("bad transaction version %d", version)
	}
	p.tx.Prefix.Version = version
	return nil
}

func (p *TxParser) readUnlockTime() error {
	unlockTime, err := binary.ReadUvarint(p.rdr)
	if err != nil {
		return err
	}
	p.tx.Prefix.TxUnlockTime = unlockTime
	return nil
}

func (p *TxParser) readVinVout() error {
	var isCoinbase = false

	// inputs
	numInputs, err := binary.ReadUvarint(p.rdr)
	if err != nil {
		return err
	}
	if numInputs == 0 {
		return fmt.Errorf("inputs length 0")
	}
	var inputs = make([]Input, numInputs)
	for i := range numInputs {
		vinType, err := p.rdr.ReadByte()
		if err != nil {
			return err
		}
		switch vinType {
		case TxinGen:
			isCoinbase = true
			height, err := binary.ReadUvarint(p.rdr)
			if err != nil {
				return err
			}
			inputs[i].Coinbase.Height = height
		case TxinToKey:
			amount, err := binary.ReadUvarint(p.rdr)
			if err != nil {
				return err
			}
			if amount != 0 {
				return fmt.Errorf("bad amount %d - v2 transactions do not publish input amounts", amount)
			}

			numMixins, err := binary.ReadUvarint(p.rdr)
			if err != nil {
				return err
			}
			var keyOffsets = make([]uint64, numMixins) //inputs[i].Key.KeyOffsets
			for mxn := range numMixins {
				keyOffset, err := binary.ReadUvarint(p.rdr)
				if err != nil {
					return err
				}
				keyOffsets[mxn] = keyOffset
			}
			inputs[i].Key.KeyOffsets = keyOffsets

		case TxinToScript, TxinToScripthash:
			return fmt.Errorf("vin type valid but old and not supported %d", vinType)
		default:
			return fmt.Errorf("vin type invalid %d", vinType)
		}

		if !isCoinbase {
			kiB := make([]byte, KeyLen)
			n, err := p.rdr.Read(kiB)
			if err != nil {
				return err
			}
			if n != KeyLen {
				return fmt.Errorf("not enough bytes read for a key image %d, expected %d", n, KeyLen)
			}
			inputs[i].Key.KeyImage = hex.EncodeToString(kiB)
		}
	}
	p.tx.Prefix.Vin = inputs

	// outputs
	numOutputs, err := binary.ReadUvarint(p.rdr)
	if err != nil {
		return err
	}
	if numOutputs == 0 {
		return fmt.Errorf("outputs length 0")
	}
	var outputs = make([]Output, numOutputs)
	for i := range numOutputs {
		amount, err := binary.ReadUvarint(p.rdr)
		if err != nil {
			return err
		}
		if !isCoinbase && amount != 0 {
			return fmt.Errorf("bad amount %d - non-coinbase v2 transactions do not publish amounts", amount)
		}
		outputs[i].AmountCoinbase = amount

		voutType, err := p.rdr.ReadByte()
		if err != nil {
			return err
		}
		switch voutType {
		case TxoutToKey:
			kiB := make([]byte, KeyLen)
			n, err := p.rdr.Read(kiB)
			if err != nil {
				return err
			}
			if n != KeyLen {
				return fmt.Errorf("not enough bytes read for a stealth key %d, expected %d", n, KeyLen)
			}
			outputs[i].Target.TaggedKey.StealthKey = hex.EncodeToString(kiB)
		case TxoutToTaggedKey:
			kiB := make([]byte, KeyLen)
			n, err := p.rdr.Read(kiB)
			if err != nil {
				return err
			}
			if n != KeyLen {
				return fmt.Errorf("not enough bytes read for a stealth key %d, expected %d", n, KeyLen)
			}
			outputs[i].Target.TaggedKey.StealthKey = hex.EncodeToString(kiB)
			tag, err := p.rdr.ReadByte()
			if err != nil {
				return err
			}
			outputs[i].Target.TaggedKey.ViewTag = hex.EncodeToString([]byte{tag})

		case TxoutToScript, TxoutToScripthash:
			return fmt.Errorf("vout type valid but not supported %d", voutType)
		default:
			return fmt.Errorf("vout type invalid %d", voutType)
		}
	}
	p.tx.Prefix.Vout = outputs
	p.tx.Coinbase = isCoinbase
	return nil
}

func (p *TxParser) readExtra() error {
	lenExtra, err := binary.ReadUvarint(p.rdr)
	if err != nil {
		return err
	}
	extraB := make([]byte, lenExtra)
	n, err := p.rdr.Read(extraB)
	if err != nil {
		return err
	}
	if n != int(lenExtra) {
		return fmt.Errorf("not all bytes read %d, expected %d", n, lenExtra)
	}
	// no attempt to parse this field beyond getting the tx_key (R) as extra
	// is a grab bag of whatever people want to put here.
	p.tx.Extra = extraB
	if extraB[0] == 1 && len(extraB) >= KeyLen+1 {
		// got tx pubkey - not important unless we later decide to decode output
		// amounts cryptographically.
		p.tx.ExtraTxKey = hex.EncodeToString(extraB[1 : 1+KeyLen])
	}
	return nil
}

// Note: not parsed is RCT Signatures, CSLAG signatures, Bulletproof plus fields
