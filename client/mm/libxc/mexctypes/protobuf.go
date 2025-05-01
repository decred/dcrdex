// This file is part of the MEXC exchange driver.
// It contains the Protocol Buffer implementation for MEXC API.

package mexctypes

import (
	"encoding/json"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
)

// Proto-related types are defined in PublicLimitDepthsV3Api.pb.go

// PublicLimitDepthsV3ApiWrapper adapts the protobuf generated struct
// with additional fields needed by our API.
type PublicLimitDepthsV3ApiWrapper struct {
	Asks         []PublicLimitDepthV3ApiItemWrapper `json:"asks,omitempty"`
	Bids         []PublicLimitDepthV3ApiItemWrapper `json:"bids,omitempty"`
	EventType    string                             `json:"eventType,omitempty"`
	Version      string                             `json:"version,omitempty"`
	Symbol       string                             `json:"symbol,omitempty"`       // Not in protobuf
	LastUpdateId uint64                             `json:"lastUpdateId,omitempty"` // Not in protobuf
}

// PublicLimitDepthV3ApiItemWrapper represents a single entry in the order book.
type PublicLimitDepthV3ApiItemWrapper struct {
	Price    string `json:"price,omitempty"`
	Quantity string `json:"quantity,omitempty"`
}

// UnmarshalMEXCDepthProto unmarshals a binary protobuf message.
// This uses proper protobuf unmarshaling using the generated code.
func UnmarshalMEXCDepthProto(data []byte) (*PublicLimitDepthsV3ApiWrapper, error) {
	// Create an instance of the protobuf generated message
	pbMsg := &PublicLimitDepthsV3Api{}

	// Unmarshal using the protobuf library
	err := proto.Unmarshal(data, pbMsg)
	if err != nil {
		return nil, err // Return error if protobuf parsing fails
	}

	// Convert from generated proto to our wrapped type
	result := &PublicLimitDepthsV3ApiWrapper{
		EventType: pbMsg.GetEventType(),
		Version:   pbMsg.GetVersion(),
	}

	// Process symbol (not part of proto but needed for our API)
	if result.EventType != "" && strings.Contains(result.EventType, "@") {
		parts := strings.Split(result.EventType, "@")
		if len(parts) > 2 {
			result.Symbol = parts[len(parts)-1]
		}
	}

	// Set LastUpdateId from Version
	if result.Version != "" {
		if updateID, err := strconv.ParseUint(result.Version, 10, 64); err == nil {
			result.LastUpdateId = updateID
		}
	}

	// Convert Asks
	if protoAsks := pbMsg.GetAsks(); len(protoAsks) > 0 {
		result.Asks = make([]PublicLimitDepthV3ApiItemWrapper, 0, len(protoAsks))
		for _, ask := range protoAsks {
			if ask != nil {
				result.Asks = append(result.Asks, PublicLimitDepthV3ApiItemWrapper{
					Price:    ask.GetPrice(),
					Quantity: ask.GetQuantity(),
				})
			}
		}
	}

	// Convert Bids
	if protoBids := pbMsg.GetBids(); len(protoBids) > 0 {
		result.Bids = make([]PublicLimitDepthV3ApiItemWrapper, 0, len(protoBids))
		for _, bid := range protoBids {
			if bid != nil {
				result.Bids = append(result.Bids, PublicLimitDepthV3ApiItemWrapper{
					Price:    bid.GetPrice(),
					Quantity: bid.GetQuantity(),
				})
			}
		}
	}

	return result, nil
}

// ConvertProtoToDepthUpdate converts from our wrapper struct to the websocket depth update format.
func ConvertProtoToDepthUpdate(pb *PublicLimitDepthsV3ApiWrapper) *WsDepthUpdateData {
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
		Symbol:  pb.Symbol,
		Version: pb.Version,
		Bids:    bids,
		Asks:    asks,
	}
}
