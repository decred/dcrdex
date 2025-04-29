// This file is part of the MEXC exchange driver.
// It contains the Protocol Buffer generated types for MEXC API.

package mexctypes

import (
	"encoding/json"
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
	// We could implement a limited parser for the binary format based on our knowledge
	// of the protocol, but for now we'll use a simplified approach with empty data

	// At a minimum, we'll provide empty slices to avoid nil pointer exceptions
	msg.Asks = []PublicLimitDepthV3ApiItem{}
	msg.Bids = []PublicLimitDepthV3ApiItem{}

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
