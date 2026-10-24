package brick

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// coerce converts a value to a declared prop type (C6).
//
// Plain strings coerce: "true" "false" "1" "0" to bool, numeric strings to
// int or float; an integral float (from JSON) becomes an int. `list` accepts
// any slice and `map` any string-keyed map. Anything else is an error naming
// the value, never a silent zero.
func coerce(v any, typ string) (any, error) {
	if v == nil || typ == typeAny || typ == "" {
		return v, nil
	}
	switch typ {
	case typeString:
		switch x := v.(type) {
		case string:
			return x, nil
		case bool, int, int64, float64:
			return fmt.Sprint(x), nil
		}
	case typeBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch x {
			case "true", "1":
				return true, nil
			case "false", "0":
				return false, nil
			}
		}
	case typeInt:
		switch x := v.(type) {
		case int:
			return int64(x), nil
		case int64:
			return x, nil
		case int32:
			return int64(x), nil
		case float64:
			if x == math.Trunc(x) && !math.IsInf(x, 0) && math.Abs(x) < 1<<62 {
				return int64(x), nil
			}
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
				return n, nil
			}
		}
	case typeFloat:
		switch x := v.(type) {
		case float64:
			return x, nil
		case float32:
			return float64(x), nil
		case int:
			return float64(x), nil
		case int64:
			return float64(x), nil
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
				return f, nil
			}
		}
	case typeList:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			return v, nil
		}
	case typeMap:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
			return v, nil
		}
	}
	return nil, fmt.Errorf("%s is not a %s", describe(v), typ)
}

// describeMax bounds how much of a value an error message quotes: enough
// to recognise it, short enough that a whole record passed where a string
// was expected does not bury the message. describeEllipsis marks the cut.
const (
	describeMax      = 60
	describeEllipsis = "..."
)

// describe renders a value for an error message.
func describe(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case nil:
		return "nothing"
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > describeMax {
		s = s[:describeMax-len(describeEllipsis)] + describeEllipsis
	}
	return fmt.Sprintf("%s (%T)", s, v)
}

// propValue is one prop as the caller gave it, before declarations apply.
type propValue struct {
	name  string
	value any
}

// applyDecls checks and completes a component's props against its `props:`
// block. It returns the final props, and the undeclared ones in source order
// for .Attrs (C5).
//
// Order of precedence (D4): the call site, then the loader, then the
// default. Loaders are merged by the caller before this runs.
func applyDecls(m *meta, given []propValue, component string) (map[string]any, []propValue, error) {
	props := make(map[string]any, len(given))
	var undeclared []propValue
	for _, pv := range given {
		d, declared := m.props[pv.name]
		if !declared {
			if m.strictProps && !strings.HasPrefix(pv.name, upPrefix) {
				return nil, nil, fmt.Errorf("prop %q is not declared by <%s>, which sets strictProps (declared: %s)", pv.name, component, strings.Join(m.order, ", "))
			}
			props[pv.name] = pv.value
			undeclared = append(undeclared, pv)
			continue
		}
		v, err := checkProp(d, pv.value)
		if err != nil {
			return nil, nil, fmt.Errorf("prop %q of <%s>: %w", pv.name, component, err)
		}
		props[pv.name] = v
	}
	for _, name := range m.order {
		d := m.props[name]
		if _, ok := props[name]; ok {
			continue
		}
		switch {
		case d.HasDefault:
			props[name] = d.Default
		case d.Required:
			return nil, nil, fmt.Errorf("<%s> requires the prop %q", component, name)
		}
	}
	return props, undeclared, nil
}

// checkProp coerces a value and checks oneof.
func checkProp(d *propDecl, v any) (any, error) {
	cv, err := d.coerce(v)
	if err != nil {
		return nil, err
	}
	if len(d.OneOf) > 0 && !slices.ContainsFunc(d.OneOf, func(o any) bool { return equalish(o, cv) }) {
		allowed := make([]string, len(d.OneOf))
		for i, o := range d.OneOf {
			allowed[i] = fmt.Sprint(o)
		}
		return nil, fmt.Errorf("%s is not one of %s", describe(cv), strings.Join(allowed, ", "))
	}
	return cv, nil
}

// equalish compares a oneof entry (decoded from YAML) with a coerced value.
func equalish(a, b any) bool {
	if a == b {
		return true
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
