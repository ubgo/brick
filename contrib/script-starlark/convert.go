package scriptstarlark

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"time"

	"go.starlark.net/starlark"
)

// errDepth is returned past the conversion depth cap (D2).
var errDepth = fmt.Errorf("value nested deeper than %d levels", maxDepth)

// toStarlark converts a Go value into Starlark (D2):
//
//	nil → None · bool → Bool · integers → Int · floats → Float · string →
//	String · []byte → Bytes · time.Time → ISO-8601 String · maps with string
//	keys → Dict, keys sorted (matching range) · slices and arrays → List ·
//	structs → Dict of exported fields · pointers → their value.
//
// seen detects cycles through pointers, maps and slices.
func toStarlark(v any, depth int, seen map[uintptr]bool) (starlark.Value, error) {
	if depth > maxDepth {
		return nil, errDepth
	}
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case starlark.Value:
		return x, nil
	case bool:
		return starlark.Bool(x), nil
	case string:
		return starlark.String(x), nil
	case []byte:
		return starlark.Bytes(string(x)), nil
	case int:
		return starlark.MakeInt(x), nil
	case int64:
		return starlark.MakeInt64(x), nil
	case int32:
		return starlark.MakeInt64(int64(x)), nil
	case uint64:
		return starlark.MakeUint64(x), nil
	case float64:
		return starlark.Float(x), nil
	case float32:
		return starlark.Float(float64(x)), nil
	case time.Time:
		return starlark.String(x.Format(time.RFC3339Nano)), nil
	case fmt.Stringer:
		if _, isJSON := v.(interface{ MarshalJSON() ([]byte, error) }); isJSON {
			return starlark.String(x.String()), nil
		}
	}
	if seen == nil {
		seen = map[uintptr]bool{}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return starlark.None, nil
		}
		if seen[rv.Pointer()] {
			return nil, errors.New("value contains a cycle")
		}
		seen[rv.Pointer()] = true
		defer delete(seen, rv.Pointer())
		return toStarlark(rv.Elem().Interface(), depth+1, seen)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("a map with %s keys cannot enter a script; keys must be strings", rv.Type().Key())
		}
		if !rv.IsNil() {
			if seen[rv.Pointer()] {
				return nil, errors.New("value contains a cycle")
			}
			seen[rv.Pointer()] = true
			defer delete(seen, rv.Pointer())
		}
		keys := make([]string, 0, rv.Len())
		for _, k := range rv.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		d := starlark.NewDict(len(keys))
		for _, k := range keys {
			ev, err := toStarlark(rv.MapIndex(reflect.ValueOf(k).Convert(rv.Type().Key())).Interface(), depth+1, seen)
			if err != nil {
				return nil, fmt.Errorf("[%q]: %w", k, err)
			}
			if err := d.SetKey(starlark.String(k), ev); err != nil {
				return nil, err
			}
		}
		return d, nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && !rv.IsNil() && rv.Len() > 0 {
			p := rv.Pointer()
			if seen[p] {
				return nil, errors.New("value contains a cycle")
			}
			seen[p] = true
			defer delete(seen, p)
		}
		elems := make([]starlark.Value, rv.Len())
		for i := range rv.Len() {
			ev, err := toStarlark(rv.Index(i).Interface(), depth+1, seen)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			elems[i] = ev
		}
		return starlark.NewList(elems), nil
	case reflect.Struct:
		t := rv.Type()
		d := starlark.NewDict(t.NumField())
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			ev, err := toStarlark(rv.Field(i).Interface(), depth+1, seen)
			if err != nil {
				return nil, fmt.Errorf(".%s: %w", f.Name, err)
			}
			if err := d.SetKey(starlark.String(f.Name), ev); err != nil {
				return nil, err
			}
		}
		return d, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return starlark.MakeInt64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return starlark.MakeUint64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return starlark.Float(rv.Float()), nil
	case reflect.String:
		return starlark.String(rv.String()), nil
	case reflect.Bool:
		return starlark.Bool(rv.Bool()), nil
	}
	return nil, fmt.Errorf("a %T cannot enter a script", v)
}

// minInt64 and maxInt64 bound script integers returned to Go (D2).
var (
	minInt64 = big.NewInt(math.MinInt64)
	maxInt64 = big.NewInt(math.MaxInt64)
)

// fromStarlark converts a script result back into brick's canonical types
// (D1, D2): None → nil, Int → int64 (outside int64 is an error), Float →
// float64 (NaN or Inf is an error), tuples and lists → []any, dicts with
// string keys → map[string]any. Sets and functions are errors.
func fromStarlark(v starlark.Value, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errDepth
	}
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.String:
		return string(x), nil
	case starlark.Bytes:
		return []byte(string(x)), nil
	case starlark.Int:
		if i, ok := x.Int64(); ok {
			return i, nil
		}
		b := x.BigInt()
		if b.Cmp(minInt64) < 0 || b.Cmp(maxInt64) > 0 {
			return nil, fmt.Errorf("an integer outside int64 (%s)", b)
		}
		return b.Int64(), nil
	case starlark.Float:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%v, which is not a number a page can use", f)
		}
		return f, nil
	case *starlark.List:
		out := make([]any, x.Len())
		for i := range x.Len() {
			ev, err := fromStarlark(x.Index(i), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	case starlark.Tuple:
		out := make([]any, len(x))
		for i, e := range x {
			ev, err := fromStarlark(e, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, item := range x.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("a dict with a %s key; keys must be strings", item[0].Type())
			}
			ev, err := fromStarlark(item[1], depth+1)
			if err != nil {
				return nil, err
			}
			out[string(k)] = ev
		}
		return out, nil
	}
	return nil, fmt.Errorf("a %s, which cannot leave a script", v.Type())
}
