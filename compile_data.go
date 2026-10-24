// Data files a page, layout or component loads: the formats brick parses and how they are normalized.

package brick

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ubgo/brick/internal/frontmatter"
)

// loadData parses a data file into brick's canonical types (D1).
func (e *Engine) loadData(p string) (any, error) {
	b, err := e.readFile(p)
	if err != nil {
		return nil, err
	}
	return parseData(p, b, e.opts.DataFormats)
}

// DataFormat parses one data file format into Go values; brick normalizes
// the result to its canonical types (D1). It is the escape hatch for formats
// the root does not bundle, such as TOML:
//
//	brick.Options{DataFormats: map[string]brick.DataFormat{".toml": func(b []byte) (any, error) {
//	    var v map[string]any
//	    return v, toml.Unmarshal(b, &v)
//	}}}
type DataFormat func(src []byte) (any, error)

// parseData decodes a data file by extension: a DataFormats entry first, so
// a host can replace even a built-in, then JSON and YAML.
func parseData(p string, b []byte, formats map[string]DataFormat) (any, error) {
	ext := strings.ToLower(pathExt(p))
	if f, ok := formats[ext]; ok {
		v, err := f(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", displayPath(p), err)
		}
		return frontmatter.Normalize(v), nil
	}
	switch ext {
	case extJSON:
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %w", displayPath(p), err)
		}
		return normalizeJSON(v), nil
	case extYAML, extYML:
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", displayPath(p), err)
		}
		return frontmatter.Normalize(v), nil
	}
	return nil, fmt.Errorf("%s: no parser for %q data; brick reads .json and .yaml, and Options.DataFormats adds others (for example .toml)", displayPath(p), ext)
}

// normalizeJSON turns json.Number into int64 or float64 (D1).
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, val := range x {
			x[k] = normalizeJSON(val)
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = normalizeJSON(val)
		}
		return x
	}
	return v
}
