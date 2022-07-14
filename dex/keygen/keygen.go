package keygen

import (
	"fmt"

	"github.com/decred/dcrd/hdkeychain/v3"
)

// RootKeyParams implements hdkeychain.NetworkParams for master
// hdkeychain.ExtendedKey creation.
type RootKeyParams struct{}

func (*RootKeyParams) HDPrivKeyVersion() [4]byte {
	return [4]byte{0x74, 0x61, 0x63, 0x6f} // ASCII "taco"
}
func (*RootKeyParams) HDPubKeyVersion() [4]byte {
	return [4]byte{0x64, 0x65, 0x78, 0x63} // ASCII "dexc"
}

// GenDeepChild derives the leaf of a path of children from a root extended key.
func GenDeepChild(seed []byte, kids []uint32) (*hdkeychain.ExtendedKey, error) {
	root, err := hdkeychain.NewMaster(seed, &RootKeyParams{})
	if err != nil {
		return nil, err
	}
	defer root.Zero()
	genChild := func(parent *hdkeychain.ExtendedKey, childIdx uint32) (*hdkeychain.ExtendedKey, error) {
		err := hdkeychain.ErrInvalidChild
		for err == hdkeychain.ErrInvalidChild {
			var kid *hdkeychain.ExtendedKey
			kid, err = parent.ChildBIP32Std(childIdx)
			if err == nil {
				return kid, nil
			}
			fmt.Printf("Child derive skipped a key index %d -> %d", childIdx, childIdx+1) // < 1 in 2^127 chance
			childIdx++
		}
		return nil, err
	}

	extKey := root
	for _, childIdx := range kids {
		childExtKey, err := genChild(extKey, childIdx)
		extKey.Zero()
		extKey = childExtKey
		if err != nil {
			extKey.Zero()
			return nil, fmt.Errorf("genChild error: %w", err)
		}
	}

	return extKey, nil
}
