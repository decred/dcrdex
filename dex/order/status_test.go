package order

import (
	"testing"
)

func TestOrderStatus_String(t *testing.T) {
	// spot check a valid status
	bookedStatusStr := OrderStatusBooked.String()
	if bookedStatusStr != "booked" {
		t.Errorf(`OrderStatusBooked.String() = %s (!= "booked")`, bookedStatusStr)
	}

	unknownStatusStr := OrderStatusUnknown.String()
	if unknownStatusStr != "unknown" {
		t.Errorf(`OrderStatusBooked.String() = %s (!= "unknown")`, unknownStatusStr)
	}

	// Check a fake and invalid status.
	os := OrderStatus(12345)
	defer func() {
		if recover() == nil {
			t.Fatalf("OrderStatus.String() should panic for invalid status")
		}
	}()
	_ = os.String()
}
