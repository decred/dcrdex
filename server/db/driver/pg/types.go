package pg

import (
	"fmt"
)

// fastUint64 provides an efficient Scanner for 64-bit integer data. With a
// regular uint64 or any other integer type that itself does not implement
// Scanner, the database/sql.Scan implementation will coerce type by converting
// to and from a string, so we avoid this expensive operation by implementing a
// Scanner that uses a type assertion. This only works for columns with a
// postgresql type of INT8 (8-byte integer) that scan a value of type int64.
type fastUint64 uint64

func (n *fastUint64) Scan(value interface{}) error {
	if value == nil {
		*n = 0
		return fmt.Errorf("NULL not supported")
	}

	v, ok := value.(int64)
	if !ok {
		return fmt.Errorf("not a signed 64-bit integer: %T", value)
	}
	*n = fastUint64(v)
	return nil

	// NOTE: If we want to be able to scan all other interger types and coerce
	// them into uint64, we can switch on each signed type. PostgreSQL has no
	// unsigned types, so just int8, int16, int32, and int64.

	// switch v := value.(type) {
	// case int64:
	// 	*n = fastUint64(v)
	// case int32:
	// 	*n = fastUint64(v)
	// case int16:
	// 	*n = fastUint64(v)
	// case int8:
	// 	*n = fastUint64(v)
	// default:
	// 	return fmt.Errorf("not a signed integer: %t", value)
	// }
	// return nil
}
