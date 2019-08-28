// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

// TODO. PLACEHOLDER. This Match is likely to be much different, especially
// depending on whether match groups are required to deal with multiple makers
// and partial fills.

type Match struct {
	Maker Order
	Taker Order
}
