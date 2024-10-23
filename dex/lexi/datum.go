// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"bytes"
	"fmt"

	"github.com/decred/dcrd/wire"
)

// datum is a value in the key-value database, along with information about
// its index entries.
type datum struct {
	version byte
	indexes [][]byte
	v       []byte
}

func (d *datum) bytes() ([]byte, error) {
	if d.version != 0 {
		return nil, fmt.Errorf("unknown datum version %d", d.version)
	}
	bLen := 1 + len(d.v) + wire.VarIntSerializeSize(uint64(len(d.v))) + wire.VarIntSerializeSize(uint64(len(d.indexes)))
	for _, ib := range d.indexes {
		bLen += len(ib) + wire.VarIntSerializeSize(uint64(len(ib)))
	}
	b := bytes.NewBuffer(make([]byte, 0, bLen))
	if err := b.WriteByte(d.version); err != nil {
		return nil, fmt.Errorf("error writing version: %w", err)
	}
	if err := wire.WriteVarInt(b, 0, uint64(len(d.indexes))); err != nil {
		return nil, fmt.Errorf("error writing index count var int: %w", err)
	}
	for _, ib := range d.indexes {
		if err := wire.WriteVarInt(b, 0, uint64(len(ib))); err != nil {
			return nil, fmt.Errorf("error writing index var int: %w", err)
		}
		if _, err := b.Write(ib); err != nil {
			return nil, fmt.Errorf("error writing index value: %w", err)
		}
	}
	if err := wire.WriteVarInt(b, 0, uint64(len(d.v))); err != nil {
		return nil, fmt.Errorf("error writing value var int: %w", err)
	}
	if _, err := b.Write(d.v); err != nil {
		return nil, fmt.Errorf("error writing value: %w", err)
	}
	return b.Bytes(), nil
}

func decodeDatum(blob []byte) (*datum, error) {
	if len(blob) < 4 {
		return nil, fmt.Errorf("datum blob length cannot be < 4. got %d", len(blob))
	}
	d := &datum{version: blob[0]}
	if d.version != 0 {
		return nil, fmt.Errorf("unknown datum blob version %d", d.version)
	}
	b := bytes.NewBuffer(blob[1:])
	nIndexes, err := wire.ReadVarInt(b, 0)
	if err != nil {
		return nil, fmt.Errorf("error reading number of indexes: %w", err)
	}
	d.indexes = make([][]byte, nIndexes)
	for i := 0; i < int(nIndexes); i++ {
		indexLen, err := wire.ReadVarInt(b, 0)
		if err != nil {
			return nil, fmt.Errorf("error reading index length: %w", err)
		}
		d.indexes[i] = make([]byte, indexLen)
		if _, err := b.Read(d.indexes[i]); err != nil {
			return nil, fmt.Errorf("error reading index: %w", err)
		}
	}
	valueLen, err := wire.ReadVarInt(b, 0)
	if err != nil {
		return nil, fmt.Errorf("erro reading value var int: %w", err)
	}
	d.v = make([]byte, valueLen)
	if _, err := b.Read(d.v); err != nil {
		return nil, fmt.Errorf("error reading value: %w", err)
	}
	return d, nil
}
