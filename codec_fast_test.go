package brick

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The allocation-free paths in codec.go and state.go must give the same
// answers and bytes as the plain versions they replaced. These tests keep
// those plain versions as references and compare.

// refMarkFloat is the full-copy markFloat the lazy one replaced.
func refMarkFloat(v any) any {
	switch x := v.(type) {
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return x
		}
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEn") {
			s += ".0"
		}
		return json.Number(s)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = refMarkFloat(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = refMarkFloat(e)
		}
		return out
	}
	return v
}

type namedInt int

// jsonCases are values on both sides of checkJSONValue's line, including
// the ones a lazy copy could get wrong: nil maps and lists (written {} and
// [], not null) and floats deep inside shared maps.
func jsonCases() map[string]any {
	return map[string]any{
		"nil":          nil,
		"scalars":      map[string]any{"s": "x", "b": true, "i": int64(3), "n": json.Number("4"), "f": 2.0, "g": 1.5e300},
		"nil map":      map[string]any{"m": map[string]any(nil), "l": []any(nil)},
		"empty":        map[string]any{"m": map[string]any{}, "l": []any{}},
		"deep float":   map[string]any{"a": map[string]any{"b": []any{int64(1), map[string]any{"c": 3.0}}}, "z": "keep"},
		"no float":     map[string]any{"a": map[string]any{"b": []any{int64(1), "two", true}}},
		"typed":        map[string]any{"ints": []int{1, 2}, "strs": map[string]string{"a": "b"}, "f32": float32(1.5), "u8": uint8(7)},
		"nan":          map[string]any{"a": []any{int64(1), math.NaN()}},
		"inf":          map[string]any{"x": map[string]any{"y": math.Inf(-1)}},
		"struct":       map[string]any{"b": 1.0, "a": struct{}{}},
		"time":         map[string]any{"when": time.Unix(0, 0)},
		"func":         []any{"ok", func() {}},
		"named scalar": map[string]any{"k": namedInt(1)},
		"int key map":  map[string]any{"m": map[int]string{1: "a"}},
		"first in key order": map[string]any{
			"b": []any{func() {}}, "a": map[string]any{"z": struct{}{}},
		},
		"typed nested bad": map[string]any{"l": []map[string]any{{"ok": 1}, {"bad": time.Time{}}}},
	}
}

func TestCheckJSONValueFastPath(t *testing.T) {
	t.Parallel()
	for name, v := range jsonCases() {
		fast := checkJSONValue("state", v)
		slow := jsonValuePath("state", v)
		if fmt.Sprint(fast) != fmt.Sprint(slow) || jsonValueOK(v) != (slow == nil) {
			t.Errorf("%s: fast %v, slow %v, ok %v", name, fast, slow, jsonValueOK(v))
		}
	}
	// The error still names the first bad value in key order.
	err := checkJSONValue("state", jsonCases()["first in key order"])
	if err == nil || !strings.Contains(err.Error(), "state.a.z: struct {}") {
		t.Fatalf("first bad value: %v", err)
	}
}

func TestMarkFloatsMatchesFullCopy(t *testing.T) {
	t.Parallel()
	for name, v := range jsonCases() {
		m, ok := v.(map[string]any)
		if !ok || !jsonValueOK(v) {
			continue
		}
		want, err := json.Marshal(refMarkFloat(m))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(markFloats(m))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	// Marking never writes into the instance's own maps.
	in := map[string]any{"a": map[string]any{"f": 2.0}, "b": []any{1.5}}
	_ = markFloats(in)
	if in["a"].(map[string]any)["f"] != 2.0 || in["b"].([]any)[0] != 1.5 {
		t.Fatalf("markFloats changed its input: %v", in)
	}
}

func TestEncodeInstanceBytesStable(t *testing.T) {
	t.Parallel()
	inst := benchInstance()
	inst.State["nested"] = map[string]any{"none": map[string]any(nil), "f": 3.0, "list": []any(nil)}
	got, err := EncodeInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"nested":{"f":3.0,"list":[],"none":{}}`) || !strings.Contains(string(got), `"score":1.5`) || !strings.Contains(string(got), `"score":0.0`) {
		t.Fatalf("encoding: %s", got)
	}
	back, err := DecodeInstance(got)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeInstance(back)
	if err != nil || string(again) != string(got) {
		t.Fatalf("round trip differs (%v):\n%s\n%s", err, got, again)
	}
}

func TestQuoteGoMatchesFmt(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "default.abc123", "sealed.AQID-_xyz", `a"b`, `a\b`, "tab\there", "nl\n", "del\x7f", "é", "\xff", "<x>&'"} {
		if got, want := quoteGo(s), fmt.Sprintf("%q", s); got != want {
			t.Errorf("quoteGo(%q) = %s, want %s", s, got, want)
		}
	}
	for c := range 256 {
		s := "x" + string(rune(c)) + string([]byte{byte(c)})
		if got, want := quoteGo(s), fmt.Sprintf("%q", s); got != want {
			t.Errorf("byte %d: %s, want %s", c, got, want)
		}
	}
}
