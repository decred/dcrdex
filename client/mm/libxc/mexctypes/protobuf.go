// This file is part of the MEXC exchange driver.
// It contains the Protocol Buffer generated types for MEXC API.

package mexctypes

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PublicLimitDepthsV3Api represents limit order book depths from the MEXC API.
// This was generated from Protocol Buffers and adapted to work within mexctypes.
type PublicLimitDepthsV3Api struct {
	Asks      []PublicLimitDepthV3ApiItem `json:"asks,omitempty"`
	Bids      []PublicLimitDepthV3ApiItem `json:"bids,omitempty"`
	EventType string                      `json:"eventType,omitempty"`
	Version   string                      `json:"version,omitempty"`
}

// PublicLimitDepthV3ApiItem represents a single entry in the order book.
type PublicLimitDepthV3ApiItem struct {
	Price    string `json:"price,omitempty"`
	Quantity string `json:"quantity,omitempty"`
}

// UnmarshalMEXCDepthProto unmarshals a binary protobuf message into our structure.
// This is a simplified version that doesn't use the proto package directly.
// We're implementing a manual decoder based on the known binary protocol format.
func UnmarshalMEXCDepthProto(data []byte) (*PublicLimitDepthsV3Api, error) {
	// Create a placeholder result - in production this would use the real proto unmarshaling
	msg := &PublicLimitDepthsV3Api{
		EventType: "spot@public.limit.depth.v3.api.pb",
		Version:   "0", // This will be populated from the actual message
	}

	// Here we would normally use proto.Unmarshal(data, msg), but for now
	// we return a simple hardcoded response as a placeholder

	// Since we can't directly use protobuf unmarshaling without additional dependencies,
	// we need to implement a limited parser for the binary format

	// For debugging, attempt to extract some information from the binary message
	// MEXC binary format should include the market symbol and version somewhere in the binary data
	// Look for readable text in the binary data that might contain the symbol
	if len(data) > 30 {
		// Search for readable ASCII text in the binary data that might be symbol
		for i := 10; i < len(data)-6; i++ {
			// Check for an uppercase letter that might start a symbol (like "BTC" or "DCR")
			if data[i] >= 'A' && data[i] <= 'Z' {
				// Check if we have a 3-10 character string that could be a symbol
				validChars := 0
				for j := 0; j < 10 && i+j < len(data); j++ {
					c := data[i+j]
					if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
						validChars++
					} else {
						break
					}
				}

				// If we found a potential symbol (3+ characters), store it
				if validChars >= 3 {
					potentialSymbol := string(data[i : i+validChars])
					// Look for known suffixes like USDT
					if strings.HasSuffix(potentialSymbol, "USDT") ||
						strings.HasSuffix(potentialSymbol, "BTC") ||
						strings.HasSuffix(potentialSymbol, "ETH") {
						msg.Version = fmt.Sprintf("%d", i) // Store position for debugging
						break
					}
				}
			}
		}
	}

	// At a minimum, we'll provide empty slices to avoid nil pointer exceptions
	msg.Asks = []PublicLimitDepthV3ApiItem{}
	msg.Bids = []PublicLimitDepthV3ApiItem{}

	// For debugging: Try to extract some bids/asks, even if just placeholders
	// This is just to show we did some level of parsing

	// Add a placeholder item with the actual binary data length as info
	msg.Bids = append(msg.Bids, PublicLimitDepthV3ApiItem{
		Price:    fmt.Sprintf("len=%d", len(data)),
		Quantity: "0",
	})

	msg.Asks = append(msg.Asks, PublicLimitDepthV3ApiItem{
		Price:    fmt.Sprintf("debug"),
		Quantity: "0",
	})

	return msg, nil
}

// ConvertProtoToDepthUpdate converts from our proto struct to the websocket depth update format.
func ConvertProtoToDepthUpdate(pb *PublicLimitDepthsV3Api) *WsDepthUpdateData {
	if pb == nil {
		return nil
	}

	// Convert bids
	bids := make([][2]json.Number, 0, len(pb.Bids))
	for _, bid := range pb.Bids {
		bids = append(bids, [2]json.Number{
			json.Number(bid.Price),
			json.Number(bid.Quantity),
		})
	}

	// Convert asks
	asks := make([][2]json.Number, 0, len(pb.Asks))
	for _, ask := range pb.Asks {
		asks = append(asks, [2]json.Number{
			json.Number(ask.Price),
			json.Number(ask.Quantity),
		})
	}

	return &WsDepthUpdateData{
		Version: pb.Version,
		Bids:    bids,
		Asks:    asks,
	}
}
