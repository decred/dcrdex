// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
)

var (
	// ErrMalformedDescriptor is returned when a provided descriptor string does
	// not match the expected format for a descriptor.
	ErrMalformedDescriptor = errors.New("malformed descriptor")
)

var (
	hardened uint32 = hdkeychain.HardenedKeyStart

	// {function}([{fingerprint}/{path}]{keyScriptOrNested})#{checksum}
	descRE = regexp.MustCompile(`([[:alnum:]]+)` + `\((?:\[([[:xdigit:]]{8})/?([\d'h/]*)\])?(\S*)\)` +
		`(?:#([[:alnum:]]{8}))?`) // https://regex101.com/r/75FEc4/1

	// {xkey}{path}{range} e.g. {tpubDCD...}{/1'/2}{/*}
	extKeyRE = regexp.MustCompile(`([[:alnum:]]+)((?:/\d+['h]?)*)(/\*['h]?)?`) // https://regex101.com/r/HLNzFF/1
)

// KeyOrigin describes the optional part of a KEY expression that contains key
// origin information in side functions like pkh(KEY). For example, in the
// descriptor wpkh([b940190e/84'/1'/0'/0/0]0300034...) the key origin is
// [b940190e/84'/1'/0'/0/0], where the first part must be 8 hexadecimal
// characters for the fingerprint of the master key, and there are zero or more
// derivation steps to reach the key. See
// https://github.com/bitcoin/bitcoin/blob/master/doc/descriptors.md#key-origin-identification
// Note for Fingerprint: "software must be willing to deal with collisions".
type KeyOrigin struct {
	// Fingerprint is "Exactly 8 hex characters for the fingerprint of the key
	// where the derivation starts".
	Fingerprint string
	// Path is "zero or more /NUM or /NUM' path elements to indicate unhardened
	// or hardened derivation steps between the fingerprint and the key or
	// xpub/xprv root that follows". ParseDescriptor will strip a leading "/".
	Path string
	// Steps is the parsed path. Hardened keys indexes are offset by 2^31.
	Steps []uint32
}

// String creates the canonical string representation of the key origin, e.g.
// [d34db33f/44'/0'/0] for a Key origin with a Fingerprint of d34db33f and a
// Path of 44'/0'/0.
func (ko *KeyOrigin) String() string {
	if ko.Path == "" {
		return fmt.Sprintf("[%s]", ko.Fingerprint)
	}
	return fmt.Sprintf("[%s/%s]", ko.Fingerprint, strings.TrimPrefix(ko.Path, "/"))
}

// KeyFmt specifies the encoding of the key within a KEY expression.
type KeyFmt byte

// Valid KeyFmt values are one of:
//  1. KeyHexPub: hex-encoded public key, compressed or uncompressed
//  2. KeyWIFPriv: WIF-encoded private key
//  3. KeyExtended public or private extended (BIP32) public key, PLUS zero or
//     more /NUM or /NUM' steps, optionally terminated with a /* or /' to
//     indicate all direct children (a range).
const (
	KeyUnknown KeyFmt = iota
	KeyWIFPriv
	KeyHexPub
	KeyExtended
)

// pubKeyBytes is a roundabout helper since ExtendedKey.pubKeyBytes() is not
// exported. The extended key should be created with an hdkeychain constructor
// so the pubkey is always valid.
func pubKeyBytes(key *hdkeychain.ExtendedKey) []byte {
	pk, _ := key.ECPubKey()
	return pk.SerializeCompressed()
}

func keyFingerprint(key *hdkeychain.ExtendedKey) []byte {
	return btcutil.Hash160(pubKeyBytes(key))[:4]
}

// ParseKeyExtended is used to decode a Descriptor.Key when KeyFmt is
// KeyExtended, which is the standard BIP32 extended key encoding with optional
// derivation steps and a range indicator appended. The fingerprint of the
// extended key is returned as a convenience. Use ParsePath to get the
// derivation steps.
// e.g. "tpubDCo.../0/*" is an extended public key with a ranged path.
func ParseKeyExtended(keyPart string) (key *hdkeychain.ExtendedKey, fingerprint, path string, isRange bool, err error) {
	xkeyParts := extKeyRE.FindStringSubmatch(keyPart)
	if len(xkeyParts) < 4 { // extKeyRE has 3 capture groups + the match itself
		return nil, "", "", false, ErrMalformedDescriptor
	}

	key, err = hdkeychain.NewKeyFromString(xkeyParts[1])
	if err != nil {
		return nil, "", "", false, err
	}
	fingerprint = hex.EncodeToString(keyFingerprint(key))
	path = strings.TrimPrefix(xkeyParts[2], "/") // extKeyRE captures the leading "/"
	isRange = len(xkeyParts[3]) > 0
	return
}

func checkDescriptorKey(key string) KeyFmt {
	// KeyWIFPriv
	_, err := btcutil.DecodeWIF(key)
	if err == nil {
		return KeyWIFPriv
	}

	// KeyHexPub
	if pkBytes, err := hex.DecodeString(key); err == nil {
		if _, err = btcec.ParsePubKey(pkBytes); err == nil {
			return KeyHexPub
		}
	}

	// KeyExtended
	if _, _, _, _, err = ParseKeyExtended(key); err == nil {
		return KeyExtended
	}

	return KeyUnknown
}

// Descriptor models the output description language used by Bitcoin Core
// descriptor wallets. Descriptors are a textual representation of an output or
// address that begins with a "function", which may take as an argument a KEY,
// SCRIPT, or other data specific to the function. This Descriptor type is
// provided to decode and represent the most common KEY descriptor types and
// SCRIPT types that commonly wrap other KEY types.
type Descriptor struct {
	// Function is the name of the top level function that begins the
	// descriptor. For example, "pk", "pkh", "wpkh", "sh", "wsh", etc.
	Function string
	// Key is set for the KEY type functions and certain SCRIPT functions with
	// nested KEY functions. May include a suffixed derivation path.
	Key string
	// KeyFmt is the type of key encoding.
	KeyFmt KeyFmt
	// KeyOrigin is an optional part of a KEY descriptor that describes the
	// derivation of the key.
	KeyOrigin *KeyOrigin
	// Nested will only be set for descriptors with SCRIPT expressions.
	Nested *Descriptor
	// Expression is the entirety of the arguments to Function. This may be a
	// KEY, SCRIPT, TREE, or combination of arguments depending on the function.
	Expression string
	// Checksum is an optional 8-character alphanumeric checksum of the
	// descriptor. It is not validated.
	Checksum string
}

func parseDescriptor(desc string, parentFn string) (*Descriptor, error) {
	parts := descRE.FindStringSubmatch(desc)
	if len(parts) < 6 { // descRE has 5 capture groups + the match itself
		return nil, ErrMalformedDescriptor
	}
	parts = parts[1:] // pop off the match itself, just check capture groups

	// function([fingerprint/path]arg)#checksum
	function, fingerprint, path := parts[0], parts[1], parts[2]
	arg, checksum := parts[3], parts[4]

	d := &Descriptor{
		Function:   function,
		Expression: arg,
		Checksum:   checksum,
	}

	switch function {
	case "pk", "pkh", "wpkh", "combo": // KEY functions
		d.Key = arg
		// Be forward compatible: key format may be KeyUnknown, but the
		// descriptor format is otherwise valid.
		d.KeyFmt = checkDescriptorKey(arg)

		if fingerprint != "" {
			steps, isRange, err := parsePath(path) // descRE discards the leading "/"
			if err != nil {
				return nil, err
			}
			if isRange {
				return nil, errors.New("range in key origin")
			}

			d.KeyOrigin = &KeyOrigin{
				Fingerprint: fingerprint,
				Path:        path,
				Steps:       steps,
			}

			// Rebuild the full argument to the key function.
			// e.g.	"[b940190e/84'/1'/0'/0/0]" + "030003...""
			d.Expression = d.KeyOrigin.String() + d.Key
		}

	// SCRIPT functions that may have nested KEY function
	case "sh":
		// sh is "top level only"
		if parentFn != "" {
			return nil, errors.New("invalid nested scripthash")
		}

		fallthrough
	case "wsh":
		// wsh is "top level or inside sh only"
		switch parentFn {
		case "", "sh":
		default:
			return nil, errors.New("invalid nested witness scripthash")
		}

		if fingerprint != "" {
			return nil, errors.New("key origin found in SCRIPT function")
		}

		var err error
		d.Nested, err = parseDescriptor(arg, function)
		if err != nil {
			return nil, err
		}
		// If child SCRIPT was a KEY function, pull it up.
		if d.Nested.KeyFmt != KeyUnknown {
			d.KeyOrigin = d.Nested.KeyOrigin
			d.Key = d.Nested.Key
			d.KeyFmt = d.Nested.KeyFmt
		}

	default: // not KEY, like multi, tr, etc.
	}

	return d, nil
}

// ParseDescriptor parses a descriptor string. See
// https://github.com/bitcoin/bitcoin/blob/master/doc/descriptors.md
// If the descriptor string does not match the expected pattern, an
// ErrMalformedDescriptor error is returned. See the Descriptor type
// documentation for information.
func ParseDescriptor(desc string) (*Descriptor, error) {
	return parseDescriptor(desc, "")
}

func parsePathPieces(pieces []string) (path []uint32, isRange bool, err error) {
	for i, p := range pieces {
		// In descriptor paths, "Anywhere a ' suffix is permitted to denote
		// hardened derivation, the suffix h can be used instead."
		if p == "*" || p == "*'" || p == "*h" {
			if i != len(pieces)-1 {
				return nil, false, errors.New("range indicator before end of path")
			}
			isRange = true
			break // end of path, this is all the addresses
		}
		pN := strings.TrimRight(p, "'h") // strip any hardened character
		pi, err := strconv.ParseUint(pN, 10, 32)
		if err != nil {
			return nil, false, err
		}
		if pN != p { // we stripped a ' or h => hardened
			pi += uint64(hardened)
		}
		path = append(path, uint32(pi))
	}
	return
}

func parsePath(p string) (path []uint32, isRange bool, err error) {
	if len(p) == 0 {
		return
	}
	pp := strings.Split(p, "/")
	return parsePathPieces(pp)
}

// ParsePath splits a common path string such as 84'/0'/0'/0 into it's
// components, returning the steps in the derivation as integers. Hardened key
// derivation steps, which are indicated by a ' or h, are offset by 2^31.
// Certain paths in descriptors may end with /* (or /*' or /*h) to indicate all
// child keys (or hardened child keys), in which case isRange will be true. As a
// special case, a prefix of "m/" or just "/" is allowed, but a master key
// fingerprint is not permitted and must be stripped first.
func ParsePath(p string) (path []uint32, isRange bool, err error) {
	p = strings.TrimPrefix(strings.TrimPrefix(p, "m"), "/")
	return parsePath(p)
}

// DeepChild derives a new extended key from the provided root extended key and
// derivation path. This is useful given a Descriptor.Key of type KeyExtended
// that may be decoded into and extended key and path with ParseKeyExtended.
// Given an address referencing the extended key via its fingerprint (also
// returned by ParseKeyExtended) and derivation path, the private key for that
// address may be derived given the child index of the address.
func DeepChild(root *hdkeychain.ExtendedKey, path []uint32) (*hdkeychain.ExtendedKey, error) {
	genChild := func(parent *hdkeychain.ExtendedKey, childIdx uint32) (*hdkeychain.ExtendedKey, error) {
		err := hdkeychain.ErrInvalidChild
		for err == hdkeychain.ErrInvalidChild {
			var kid *hdkeychain.ExtendedKey
			kid, err = parent.Derive(childIdx)
			if err == nil {
				return kid, nil
			}
			fmt.Printf("Child derive skipped a key index %d -> %d", childIdx, childIdx+1) // < 1 in 2^127 chance
			childIdx++
		}
		return nil, err
	}

	extKey := root
	for i, childIdx := range path {
		childExtKey, err := genChild(extKey, childIdx)
		if i > 0 { // don't zero the input key, root
			extKey.Zero()
		}
		extKey = childExtKey
		if err != nil {
			if i > 0 {
				extKey.Zero()
			}
			return nil, err
		}
	}

	return extKey, nil
}
