package dcr

import (
	"errors"
	"fmt"

	"github.com/decred/dcrd/crypto/ripemd160"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
)

type AddressPool struct {
	*pkhPool
	addrParams dcrutil.AddressParams
	storeIdx   func(uint32) // e.g. callback to a DB update
}

type HDKeyIndexer interface {
	KeyIndex(pubKey []byte) (uint32, error)
	SetKeyIndex(idx uint32, pubKey []byte) error
}

func NewAddressPool(acctXPub string, size uint32, keyIndexer HDKeyIndexer /*, net dex.Network */) (*AddressPool, error) {
	key, err := hdkeychain.NewKeyFromString(acctXPub, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing master pubkey: %w", err)
	}
	external, err := key.Child(0)
	if err != nil {
		return nil, err
	}
	pubkey := external.SerializedPubKey()
	next, err := keyIndexer.KeyIndex(pubkey)
	if err != nil {
		return nil, err
	}
	hashPool := newPkhPool(external, size, next)
	storKey := func(idx uint32) {
		if err := keyIndexer.SetKeyIndex(idx, pubkey); err != nil {
			fmt.Println(err)
		}
	}
	return &AddressPool{
		pkhPool:    hashPool,
		addrParams: chainParams,
		storeIdx:   storKey,
	}, nil
}

// func (ap *AddressPool) PubKeyBytes() []byte {
// 	return ap.pkhPool.xpub.SerializedPubKey()
// }

func (ap *AddressPool) Get() string {
	next := ap.next
	hash, i := ap.get()
	// Record index if it is higher than previous best.
	if i >= next {
		ap.storeIdx(i)
	}
	addr, err := dcrutil.NewAddressPubKeyHash(hash[:], ap.addrParams, dcrec.STEcdsaSecp256k1)
	if err != nil {
		panic(err.Error()) // only errors if hash is not 20 bytes (ripemd160.Size), but we guarantee that
	}
	addrStr := addr.Address()
	fmt.Printf("Retrieved address index %v: %v\n", i, addrStr)
	return addrStr
}

func (ap *AddressPool) Owns(address string) bool {
	addr, err := dcrutil.DecodeAddress(address, ap.addrParams)
	if err != nil {
		return false
	}
	hash := addr.Hash160()
	if hash == nil {
		return false // dcrutil concrete types shouldn't do this
	}
	return ap.owns(*hash)
}

func (ap *AddressPool) Used(address string) {
	addr, err := dcrutil.DecodeAddress(address, ap.addrParams)
	if err != nil {
		fmt.Printf("(*AddressPool).Used: bad address %v: %v\n", address, err)
		return
	}
	hash := addr.Hash160()
	fmt.Printf("Marking address %v (hash160 %x) used\n", addr.Address(), hash)
	ap.used(*hash)
}

// type pubkey [secp256k1.PubKeyBytesLenCompressed]byte
type h160 [ripemd160.Size]byte

type pkhPool struct {
	xpub  *hdkeychain.ExtendedKey
	size  uint32 // size of pool
	pool  map[h160]uint32
	next  uint32          // next child index to generate
	owned map[h160]uint32 // pool plus used
}

func newPkhPool(key *hdkeychain.ExtendedKey, size, next uint32) *pkhPool {
	ap := &pkhPool{
		xpub:  key,
		pool:  make(map[h160]uint32, size),
		owned: make(map[h160]uint32, next+size),
		size:  size, // must not be zero
	}

	// Fill out owned up to next.
	for ap.next < next {
		child, i := getChild(ap.xpub, ap.next)
		if child == nil {
			panic("can't make child key")
		}

		pkb := child.SerializedPubKey()
		hashBytes := dcrutil.Hash160(pkb)
		var hash h160
		copy(hash[:], hashBytes)
		ap.owned[hash] = i
		ap.next = i + 1
		// pool is empty to start. Next get() will derive new for pool.
	}

	return ap
}

func (ap *pkhPool) get() (hash h160, i uint32) {
	// Only derive if pool hasn't filled yet.
	if len(ap.pool) < int(ap.size) {
		// Generate a new one for the pool.
		var child *hdkeychain.ExtendedKey
		child, i = getChild(ap.xpub, ap.next)
		pkb := child.SerializedPubKey()
		hashBytes := dcrutil.Hash160(pkb)
		copy(hash[:], hashBytes)
		ap.pool[hash] = i
		ap.owned[hash] = i
		ap.next = i + 1
		return hash, i
	}

	// Grab a random one from the pool.
	for hash, i = range ap.pool {
		break
	}
	return hash, i
}

func (ap *pkhPool) owns(hash h160) bool {
	// First check the smaller pool, then the full set.
	_, found := ap.pool[hash]
	if !found {
		_, found = ap.owned[hash]
	}
	return found
}

func (ap *pkhPool) used(hash h160) {
	if idx, found := ap.pool[hash]; found {
		delete(ap.pool, hash)
		fmt.Printf("Deleting hash160 %x, index %d from pool\n", hash, idx)
	}
	// If delete removed it, next get() will derive new for pool.
}

func getChild(xkey *hdkeychain.ExtendedKey, i uint32) (*hdkeychain.ExtendedKey, uint32) {
	for {
		child, err := xkey.Child(i)
		switch {
		case errors.Is(err, hdkeychain.ErrInvalidChild):
			i++
			continue
		case err == nil:
			return child, i
		default:
			panic(err.Error()) // bad xkey or something
		}
	}
}
