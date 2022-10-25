// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

// BooleanConfig is set in a SwapOption to indicate that an options is a simple
// boolean.
type BooleanConfig struct {
	// Reason should summarize the difference between on/off states.
	Reason string `json:"reason"`
}

// XYRange is an option in which the user's setting of one value is reflected in
// the other and possible values are restricted. The assumed relationship is
// linear, e.g. y = mx + b. The value submitted to FundOrder/Swap/Redeem should
// specify the x value.
type XYRange struct {
	Start XYRangePoint `json:"start"`
	End   XYRangePoint `json:"end"`
	XUnit string       `json:"xUnit"`
	YUnit string       `json:"yUnit"`
}

// XYRangePoint is a point specifying the start or end of an XYRange.
type XYRangePoint struct {
	Label string  `json:"label"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// OrderOption is an available option for an order.
type OrderOption struct {
	ConfigOption

	// Fields below are mutually exclusive. The consumer should use nilness to
	// determine what type of option to display.

	// Boolean is a boolean option with two custom labels.
	Boolean *BooleanConfig `json:"boolean,omitempty"`
	// Range indicates a numeric input where the user can adjust the value
	// between a pre-defined low and high.
	XYRange *XYRange `json:"xyRange,omitempty"`
}
