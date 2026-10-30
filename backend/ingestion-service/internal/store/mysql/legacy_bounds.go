package mysql

import (
	"errors"
	"reflect"
	"time"
)

var ErrLegacyEvidenceLimit = errors.New("legacy evidence exceeds aggregate byte or structure limit")

// boundLegacyEvidence runs before JSON decoding/canonicalization/provenance
// allocation. It conservatively budgets JSON escaping, container overhead and
// every exported field of the fixed LegacyImport model, including unused tables.
// A byte slice may become base64 or RawMessage; six bytes per input byte bounds
// both. This intentionally may reject evidence whose compact JSON would fit.
// It cannot bound an earlier caller's allocation: export/CLI input needs its own
// limited reader before decoding into LegacyImport.
func boundLegacyEvidence(in LegacyImport, limit int) error {
	if limit < 1 || limit > 16<<20 {
		return ErrLegacyEvidenceLimit
	}
	remaining := limit
	charge := func(n int) bool {
		if n < 0 || n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	text := func(n int) bool {
		if n > remaining/6 {
			return false
		}
		return charge(n * 6)
	}
	var visit func(reflect.Value, int) bool
	visit = func(v reflect.Value, depth int) bool {
		if depth > 32 || !charge(64) {
			return false
		}
		if !v.IsValid() {
			return true
		}
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return true
		}
		switch v.Kind() {
		case reflect.String:
			return text(v.Len())
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return true
		case reflect.Pointer:
			return v.IsNil() || visit(v.Elem(), depth+1)
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return text(v.Len())
			}
			if v.Len() > remaining/64 {
				return false
			}
			for j := 0; j < v.Len(); j++ {
				if !visit(v.Index(j), depth+1) {
					return false
				}
			}
			return true
		case reflect.Map:
			if v.Len() > remaining/128 {
				return false
			}
			it := v.MapRange()
			for it.Next() {
				if !visit(it.Key(), depth+1) || !visit(it.Value(), depth+1) {
					return false
				}
			}
			return true
		case reflect.Struct:
			for j := 0; j < v.NumField(); j++ {
				field := v.Type().Field(j)
				if field.PkgPath != "" {
					return false
				} // New opaque types need explicit review.
				if !text(len(field.Name)) || !visit(v.Field(j), depth+1) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	if !visit(reflect.ValueOf(in), 0) {
		return ErrLegacyEvidenceLimit
	}
	return nil
}
