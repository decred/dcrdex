// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/encode"
)

// FieldParser converts positional args starting at position 0 into a field
// value. Returns the value to set, the number of args consumed, and any error.
// A zero reflect.Value means "leave the field at its zero value".
type FieldParser func(args []string) (val reflect.Value, consumed int, err error)

// routeOverride specifies custom behavior for a route.
type routeOverride struct {
	fieldParsers map[string]FieldParser      // JSON field name → custom parser
	postProcess  func(v reflect.Value) error // called after all fields are set
}

// routeOverrides maps route names to their custom parsing behavior.
var routeOverrides = map[string]*routeOverride{
	"newwallet": {
		fieldParsers: map[string]FieldParser{
			"config": parseConfigFromRemainingArgs,
		},
	},
	"multitrade": {
		fieldParsers: map[string]FieldParser{
			"placement": parsePlacementsJSON,
		},
	},
	"startmmbot": {
		fieldParsers: map[string]FieldParser{
			"market": parseOptionalMarketFilter,
		},
	},
	"stopmmbot": {
		fieldParsers: map[string]FieldParser{
			"market": parseOptionalMarketFilter,
		},
	},
	"updaterunningbotcfg": {
		fieldParsers: map[string]FieldParser{
			"market":   parseRequiredMarketFilter,
			"balances": parseOptionalInventory,
		},
	},
	"updaterunningbotinv": {
		fieldParsers: map[string]FieldParser{
			"market":   parseRequiredMarketFilter,
			"balances": parseRequiredInventory,
		},
	},
	"postbond": {
		fieldParsers: map[string]FieldParser{
			"maintainTier": parseBoolDefaultTrue,
		},
	},
	"withdraw": {
		postProcess: func(v reflect.Value) error {
			fv := fieldByJSONName(v, "subtract")
			if fv.IsValid() {
				fv.SetBool(true)
			}
			return nil
		},
	},
}

// buildPayload converts positional CLI args and passwords into a typed params
// struct for the given route. Returns nil payload for routes that take no
// params.
func buildPayload(route string, pws []encode.PassBytes, args []string) (any, error) {
	if !rpcserver.RouteExists(route) {
		return nil, fmt.Errorf("unknown route %q", route)
	}
	return buildFromStruct(route, pws, args)
}

// buildFromStruct uses reflection to create and populate a params struct from
// positional CLI args and passwords. Field order matches struct declaration
// order (same as help output).
func buildFromStruct(route string, pws []encode.PassBytes, args []string) (any, error) {
	pt := rpcserver.ParamType(route)
	if pt == nil {
		return nil, nil // no-params route
	}

	pv := reflect.New(pt)
	v := pv.Elem()
	fields := rpcserver.ReflectFields(pt)
	overrides := routeOverrides[route]

	pwIdx := 0
	argIdx := 0

	for _, fi := range fields {
		fv := fieldByJSONName(v, fi.JSONName)
		if !fv.IsValid() {
			continue
		}

		// Password fields are filled from the passwords slice.
		if fi.IsPassword {
			if pwIdx >= len(pws) {
				return nil, fmt.Errorf("missing password for %s", fi.JSONName)
			}
			fv.Set(reflect.ValueOf(pws[pwIdx]))
			pwIdx++
			continue
		}

		// Check for a custom field parser.
		if overrides != nil {
			if parser, ok := overrides.fieldParsers[fi.JSONName]; ok {
				remaining := args[argIdx:]
				val, consumed, err := parser(remaining)
				if err != nil {
					return nil, fmt.Errorf("bad %s: %w", fi.JSONName, err)
				}
				if val.IsValid() {
					fv.Set(val)
				}
				argIdx += consumed
				continue
			}
		}

		// Standard field: consume one arg and convert.
		s := ""
		if argIdx < len(args) {
			s = args[argIdx]
		}

		if s == "" {
			// No more args or empty arg — skip if the field is optional.
			if fi.IsOptional || isEffectivelyOptional(fi.GoType) {
				if argIdx < len(args) {
					argIdx++ // consume the empty arg
				}
				continue
			}
			return nil, fmt.Errorf("missing %s", fi.JSONName)
		}

		if err := setFieldFromString(fv, fi.GoType, s); err != nil {
			return nil, fmt.Errorf("bad %s: %w", fi.JSONName, err)
		}
		argIdx++
	}

	// Run postProcess if registered.
	if overrides != nil && overrides.postProcess != nil {
		if err := overrides.postProcess(v); err != nil {
			return nil, err
		}
	}

	return pv.Interface(), nil
}

// isEffectivelyOptional returns true for types whose zero value is a valid
// "not provided" sentinel (maps, slices, interfaces).
// NOTE: This means all interface{}/any fields are treated as optional. If a
// required any-typed field is ever added, this should be revisited to use
// struct tags (e.g. omitempty) instead of type-based inference.
func isEffectivelyOptional(t reflect.Type) bool {
	if t == nil {
		return true
	}
	k := t.Kind()
	return k == reflect.Map || k == reflect.Slice || k == reflect.Interface
}

// fieldByJSONName returns the reflect.Value for the struct field with the given
// JSON tag name, recursing into embedded (anonymous) structs.
func fieldByJSONName(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.Anonymous {
			fv := v.Field(i)
			if fv.Kind() == reflect.Ptr {
				if fv.IsNil() {
					fv.Set(reflect.New(sf.Type.Elem()))
				}
				fv = fv.Elem()
			}
			if fv.Kind() == reflect.Struct {
				if found := fieldByJSONName(fv, name); found.IsValid() {
					return found
				}
			}
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		jname := tag
		if idx := strings.IndexByte(tag, ','); idx != -1 {
			jname = tag[:idx]
		}
		if jname == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

// setFieldFromString converts a string CLI arg to the appropriate Go type and
// sets the struct field value.
func setFieldFromString(fv reflect.Value, goType reflect.Type, s string) error {
	// Handle pointers: allocate and set the element.
	if goType.Kind() == reflect.Ptr {
		pv := reflect.New(goType.Elem())
		if err := setFieldFromString(pv.Elem(), goType.Elem(), s); err != nil {
			return err
		}
		fv.Set(pv)
		return nil
	}

	// Handle interface{} / any.
	if goType.Kind() == reflect.Interface {
		fv.Set(reflect.ValueOf(s))
		return nil
	}

	switch goType.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		v, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bits := int(goType.Size()) * 8
		v, err := strconv.ParseUint(s, 10, bits)
		if err != nil {
			return err
		}
		fv.SetUint(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := int(goType.Size()) * 8
		v, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return err
		}
		fv.SetInt(v)
	case reflect.Map:
		mp := reflect.New(goType)
		if err := json.Unmarshal([]byte(s), mp.Interface()); err != nil {
			return err
		}
		fv.Set(mp.Elem())
	case reflect.Slice:
		sp := reflect.New(goType)
		if err := json.Unmarshal([]byte(s), sp.Interface()); err != nil {
			return err
		}
		fv.Set(sp.Elem())
	// NOTE: float32/float64 are not currently handled. Add a case here if a
	// future params struct uses floating-point fields.
	default:
		return fmt.Errorf("unsupported field type %v", goType)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseUint32(s string) (uint32, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	return uint32(v), err
}

// parseInventoryArg parses a JSON-encoded map[uint32]int64 from a string like
// [[assetID, amount], ...].
func parseInventoryArg(s string) (map[uint32]int64, error) {
	if s == "" {
		return nil, nil
	}
	var pairs [][2]int64
	if err := json.Unmarshal([]byte(s), &pairs); err != nil {
		return nil, fmt.Errorf("error parsing inventory JSON: %w", err)
	}
	m := make(map[uint32]int64, len(pairs))
	for _, p := range pairs {
		m[uint32(p[0])] = p[1]
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Custom field parsers
// ---------------------------------------------------------------------------

// parseConfigFromRemainingArgs consumes all remaining args as key=value pairs
// or JSON objects and returns a map[string]string.
func parseConfigFromRemainingArgs(args []string) (reflect.Value, int, error) {
	if len(args) == 0 {
		return reflect.Value{}, 0, nil
	}
	cfg := make(map[string]string)
	for _, a := range args {
		if strings.HasPrefix(a, "{") {
			var jm map[string]string
			if err := json.Unmarshal([]byte(a), &jm); err != nil {
				return reflect.Value{}, 0, fmt.Errorf("bad JSON config arg: %w", err)
			}
			for k, v := range jm {
				cfg[k] = v
			}
		} else if k, v, ok := strings.Cut(a, "="); ok {
			cfg[k] = v
		} else {
			return reflect.Value{}, 0, fmt.Errorf("unrecognized config arg %q (expected key=value or JSON)", a)
		}
	}
	if len(cfg) == 0 {
		return reflect.Value{}, len(args), nil
	}
	return reflect.ValueOf(cfg), len(args), nil
}

// parsePlacementsJSON parses JSON-encoded [[qty,rate],...] into []*core.QtyRate.
func parsePlacementsJSON(args []string) (reflect.Value, int, error) {
	if len(args) == 0 {
		return reflect.Value{}, 0, fmt.Errorf("missing placements")
	}
	var rawPlacements [][2]uint64
	if err := json.Unmarshal([]byte(args[0]), &rawPlacements); err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad placements JSON: %w", err)
	}
	placements := make([]*core.QtyRate, len(rawPlacements))
	for i, rp := range rawPlacements {
		placements[i] = &core.QtyRate{Qty: rp[0], Rate: rp[1]}
	}
	return reflect.ValueOf(placements), 1, nil
}

// parseOptionalMarketFilter parses 0 or 3 args into a *mm.MarketWithHost.
func parseOptionalMarketFilter(args []string) (reflect.Value, int, error) {
	if len(args) == 0 {
		return reflect.Value{}, 0, nil
	}
	if len(args) < 3 {
		return reflect.Value{}, 0, fmt.Errorf(
			"market filter requires host, baseID, and quoteID (got %d of 3)", len(args))
	}
	baseID, err := parseUint32(args[1])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad baseID: %w", err)
	}
	quoteID, err := parseUint32(args[2])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad quoteID: %w", err)
	}
	mkt := &mm.MarketWithHost{
		Host:    args[0],
		BaseID:  baseID,
		QuoteID: quoteID,
	}
	return reflect.ValueOf(mkt), 3, nil
}

// parseRequiredMarketFilter parses exactly 3 args into a mm.MarketWithHost.
func parseRequiredMarketFilter(args []string) (reflect.Value, int, error) {
	if len(args) < 3 {
		return reflect.Value{}, 0, fmt.Errorf("need host, baseID, and quoteID")
	}
	baseID, err := parseUint32(args[1])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad baseID: %w", err)
	}
	quoteID, err := parseUint32(args[2])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad quoteID: %w", err)
	}
	mkt := mm.MarketWithHost{
		Host:    args[0],
		BaseID:  baseID,
		QuoteID: quoteID,
	}
	return reflect.ValueOf(mkt), 3, nil
}

// parseOptionalInventory parses 0 or 2 args into a *mm.BotInventoryDiffs.
func parseOptionalInventory(args []string) (reflect.Value, int, error) {
	if len(args) == 0 {
		return reflect.Value{}, 0, nil
	}
	if len(args) < 2 {
		return reflect.Value{}, 0, fmt.Errorf("need both dexInventory and cexInventory")
	}
	return parseInventory(args)
}

// parseRequiredInventory parses exactly 2 args into a *mm.BotInventoryDiffs.
func parseRequiredInventory(args []string) (reflect.Value, int, error) {
	if len(args) < 2 {
		return reflect.Value{}, 0, fmt.Errorf("need dexInventory and cexInventory")
	}
	return parseInventory(args)
}

func parseInventory(args []string) (reflect.Value, int, error) {
	dexInv, err := parseInventoryArg(args[0])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad dexInventory: %w", err)
	}
	cexInv, err := parseInventoryArg(args[1])
	if err != nil {
		return reflect.Value{}, 0, fmt.Errorf("bad cexInventory: %w", err)
	}
	inv := &mm.BotInventoryDiffs{
		DEX: dexInv,
		CEX: cexInv,
	}
	return reflect.ValueOf(inv), 2, nil
}

// parseBoolDefaultTrue parses an optional bool that defaults to true when
// omitted.
func parseBoolDefaultTrue(args []string) (reflect.Value, int, error) {
	v := true // Escapes to heap; reflect.ValueOf(&v) intentionally captures its address.
	if len(args) > 0 && args[0] != "" {
		var err error
		v, err = strconv.ParseBool(args[0])
		if err != nil {
			return reflect.Value{}, 0, err
		}
		return reflect.ValueOf(&v), 1, nil
	}
	return reflect.ValueOf(&v), 0, nil
}
