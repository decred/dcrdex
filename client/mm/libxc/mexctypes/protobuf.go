// This file is part of the MEXC exchange driver.
// It contains the Protocol Buffer implementation for MEXC API.

package mexctypes

import (
	"encoding/json"
	"fmt"
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

// Price scaling factor for MEXC API
// Values from the API are multiplied by these factors
const (
	mexcPriceScaleFactor    = 1000000.0   // 10^6 for price values
	mexcQuantityScaleFactor = 100000000.0 // 10^8 for quantity values
)

// UnmarshalMEXCDepthProto unmarshals a binary protobuf message.
// This handles parsing the websocket message format and extracting the protobuf data.
func UnmarshalMEXCDepthProto(data []byte) (*PublicLimitDepthsV3ApiWrapper, error) {
	// Check if this is a JSON message first - MEXC may send control messages as JSON
	if len(data) > 0 && data[0] == '{' {
		// This is likely a JSON message, not a protobuf message
		var wsMsg WsMessage
		if err := json.Unmarshal(data, &wsMsg); err == nil {
			// Successfully parsed as JSON - extract info if possible
			return handleJSONMessage(wsMsg)
		}
		// If JSON parsing failed, continue and try protobuf parsing
	}

	// Try to extract protobuf data from the websocket message
	// The actual protobuf data might be wrapped or have headers
	protoData, err := extractProtobufData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to extract protobuf data: %w", err)
	}

	// Create an instance of the protobuf generated message
	pbMsg := &PublicLimitDepthsV3Api{}

	// Unmarshal using the protobuf library
	err = proto.Unmarshal(protoData, pbMsg)
	if err != nil {
		return nil, fmt.Errorf("proto: cannot parse data: %w", err)
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

	// Convert Asks with correct scaling factor
	if protoAsks := pbMsg.GetAsks(); len(protoAsks) > 0 {
		result.Asks = make([]PublicLimitDepthV3ApiItemWrapper, 0, len(protoAsks))
		for _, ask := range protoAsks {
			if ask != nil {
				// Apply scaling factor to convert from MEXC's representation to our API format
				price, qty := applyScalingFactor(ask.GetPrice(), ask.GetQuantity())
				result.Asks = append(result.Asks, PublicLimitDepthV3ApiItemWrapper{
					Price:    price,
					Quantity: qty,
				})
			}
		}
	}

	// Convert Bids with correct scaling factor
	if protoBids := pbMsg.GetBids(); len(protoBids) > 0 {
		result.Bids = make([]PublicLimitDepthV3ApiItemWrapper, 0, len(protoBids))
		for _, bid := range protoBids {
			if bid != nil {
				// Apply scaling factor to convert from MEXC's representation to our API format
				price, qty := applyScalingFactor(bid.GetPrice(), bid.GetQuantity())
				result.Bids = append(result.Bids, PublicLimitDepthV3ApiItemWrapper{
					Price:    price,
					Quantity: qty,
				})
			}
		}
	}

	return result, nil
}

// applyScalingFactor adjusts price and quantity values from MEXC's representation
// to the format expected by our API.
func applyScalingFactor(priceStr, qtyStr string) (string, string) {
	// Parse the string values to floats
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		// If parsing fails, return the original strings
		return priceStr, qtyStr
	}

	qty, err := strconv.ParseFloat(qtyStr, 64)
	if err != nil {
		// If parsing fails, return the original strings
		return priceStr, qtyStr
	}

	// Divide by the scaling factors to get the actual values
	adjustedPrice := price / mexcPriceScaleFactor
	adjustedQty := qty / mexcQuantityScaleFactor

	// Format price with 5 decimal places and quantity with 8 decimal places
	return strconv.FormatFloat(adjustedPrice, 'f', 5, 64),
		strconv.FormatFloat(adjustedQty, 'f', 8, 64)
}

// extractProtobufData attempts to extract the actual protobuf binary data from the websocket message.
// MEXC may send the data in a specific format with headers or metadata.
func extractProtobufData(data []byte) ([]byte, error) {
	// Handle different possibilities for message format:

	// 1. If the message is very short, it's probably not a valid protobuf message
	if len(data) < 10 {
		return nil, fmt.Errorf("message too short to be valid protobuf: %d bytes", len(data))
	}

	// 2. The message might have a header or specific framing
	// Common patterns in binary protocols:
	// - First few bytes might be a message type or length
	// - There might be a specific offset where the actual data starts

	// Try with potential binary frame formats:

	// Option 1: Skip first 4 bytes (potential header/length)
	if len(data) > 4 {
		protoData := data[4:]
		if tryUnmarshal(protoData) {
			return protoData, nil
		}
	}

	// Option 2: Skip first 8 bytes
	if len(data) > 8 {
		protoData := data[8:]
		if tryUnmarshal(protoData) {
			return protoData, nil
		}
	}

	// Option 3: Check for specific signature/magic bytes
	// Example: If we know the protobuf often starts with specific byte patterns
	for i := 0; i < len(data)-8; i++ {
		// Look for potential protobuf message start
		// (This is speculative - ideally we'd know the exact framing)
		if (data[i] == 0x0A || data[i] == 0x12) && (data[i+1] < 0x80) {
			protoData := data[i:]
			if tryUnmarshal(protoData) {
				return protoData, nil
			}
		}
	}

	// If all the above options failed, try using the raw data
	if tryUnmarshal(data) {
		return data, nil
	}

	// If we can identify a JSON part at the beginning, skip it
	for i := 0; i < len(data)-1; i++ {
		if data[i] == '}' && (data[i+1] == 0x0A || data[i+1] == 0x12) {
			protoData := data[i+1:]
			if tryUnmarshal(protoData) {
				return protoData, nil
			}
		}
	}

	// If all options failed, return the original data
	// This will likely fail to unmarshal, but we've tried our best
	return data, nil
}

// tryUnmarshal attempts to unmarshal a protobuf message to check if it's valid.
// This is used to validate potential message formats.
func tryUnmarshal(data []byte) bool {
	msg := &PublicLimitDepthsV3Api{}
	// We don't care about the content, just whether it can be parsed without error
	return proto.Unmarshal(data, msg) == nil
}

// handleJSONMessage processes a JSON message from the websocket.
// This handles control messages or other non-protobuf data.
func handleJSONMessage(msg WsMessage) (*PublicLimitDepthsV3ApiWrapper, error) {
	// Create a placeholder result
	result := &PublicLimitDepthsV3ApiWrapper{
		EventType: msg.Channel,
		Symbol:    msg.Symbol,
		Version:   strconv.FormatInt(msg.Ts, 10),
	}

	// If there's a timestamp, use it as LastUpdateId
	if msg.Ts > 0 {
		result.LastUpdateId = uint64(msg.Ts)
	}

	// If this has actual data attached, try to parse it
	if len(msg.Data) > 0 {
		// In some cases, the Data field might contain a JSON-encoded depth update
		var depthData PublicLimitDepthsV3ApiWrapper
		if err := json.Unmarshal(msg.Data, &depthData); err == nil {
			// If we successfully parsed depth data, use that
			if len(depthData.Asks) > 0 {
				// Apply scaling to the JSON data as well
				scaledAsks := make([]PublicLimitDepthV3ApiItemWrapper, 0, len(depthData.Asks))
				for _, ask := range depthData.Asks {
					price, qty := applyScalingFactor(ask.Price, ask.Quantity)
					scaledAsks = append(scaledAsks, PublicLimitDepthV3ApiItemWrapper{
						Price:    price,
						Quantity: qty,
					})
				}
				result.Asks = scaledAsks
			}
			if len(depthData.Bids) > 0 {
				// Apply scaling to the JSON data as well
				scaledBids := make([]PublicLimitDepthV3ApiItemWrapper, 0, len(depthData.Bids))
				for _, bid := range depthData.Bids {
					price, qty := applyScalingFactor(bid.Price, bid.Quantity)
					scaledBids = append(scaledBids, PublicLimitDepthV3ApiItemWrapper{
						Price:    price,
						Quantity: qty,
					})
				}
				result.Bids = scaledBids
			}
			if depthData.Version != "" {
				result.Version = depthData.Version
			}
			if depthData.LastUpdateId > 0 {
				result.LastUpdateId = depthData.LastUpdateId
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
