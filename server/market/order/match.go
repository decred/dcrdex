package order

// TODO. PLACEHOLDER. This Match is likely to be much different, especially
// depending on whether match groups are required to deal with multiple makers
// and partial fills.

type Match struct {
	Maker Order
	Taker Order
}
