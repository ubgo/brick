package brick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// instanceFormat is the version of the stable instance encoding
// . A decoder meeting another version reports "not
// found", so the component recovers in place instead of guessing.
const instanceFormat = 1

// Codec turns an Instance into bytes and back, for every store that keeps
// bytes (Bytes, SealedStates). The default is JSONCodec; replace it
// (Options.Codec, or per store) when the host's state holds values JSON
// cannot carry, or for smaller blobs (msgpack, CBOR).
type Codec interface {
	Encode(*Instance) ([]byte, error)
	Decode([]byte) (*Instance, error)
}

// JSONCodec is the stable JSON v1 encoding (EncodeInstance,
// DecodeInstance), the default everywhere: a Redis written by one service
// is readable by another.
type JSONCodec struct{}

// Encode is EncodeInstance.
func (JSONCodec) Encode(inst *Instance) ([]byte, error) { return EncodeInstance(inst) }

// Decode is DecodeInstance.
func (JSONCodec) Decode(b []byte) (*Instance, error) { return DecodeInstance(b) }

// ErrStateValue marks an instance that cannot be stored as it is — a value
// JSON cannot carry, or a sealed blob over MaxBytes. It is the component's
// fault, reported at render, never mistaken for a store outage.
var ErrStateValue = errors.New("brick: state cannot be stored")

// errUnknownFormat marks a blob written in a format this build does not
// read; stores treat it as "not found" (recovery), never as a 500.
var errUnknownFormat = errors.New("brick: unknown state encoding version")

// wireInstance is the JSON v1 shape. Field names are short and frozen:
// every shared store (Redis, SQL, a sealed blob) holds exactly this, so
// old and new servers in a rolling deploy read each other's instances.
// The handle is not encoded: it is the store's key, never the value.
type wireInstance struct {
	V     int            `json:"v"`
	Store string         `json:"store,omitempty"`
	Token string         `json:"token"`
	C     wireComponent  `json:"c"`
	Props map[string]any `json:"props,omitempty"`
	Ctx   map[string]any `json:"ctx,omitempty"`
	State map[string]any `json:"state"`
	Keys  []string       `json:"keys,omitempty"`
	Touch []string       `json:"touched,omitempty"`
	Pend  []wirePending  `json:"pending,omitempty"`
	// Slots is Instance.Slots; omitted when the call wrote none, so every
	// instance stored before slots existed still reads the same.
	Slots map[string]string `json:"slots,omitempty"`
	Sess  string            `json:"sess,omitempty"`
	Ver   int64             `json:"ver"`
	Iat   string            `json:"iat,omitempty"`
}

// wirePending is one presigned upload id (Instance.Pending).
type wirePending struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Iat   string `json:"iat"`
}

type wireComponent struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	ID     string `json:"id,omitempty"`
	Page   string `json:"page,omitempty"`
	Locale string `json:"locale,omitempty"`
	// Key is Instance.Key; omitted when the call had no key=.
	Key string `json:"key,omitempty"`
	// Route and Parts are Instance.Route and RouteParts; omitted on a
	// static page.
	Route map[string]string `json:"route,omitempty"`
	Parts []string          `json:"parts,omitempty"`
}

// EncodeInstance is the stable JSON v1 encoding of an instance
// . Map keys are sorted, so equal instances encode to
// equal bytes. A value that is not JSON-compatible is an error naming its
// path, never a silently lossy encoding.
func EncodeInstance(inst *Instance) ([]byte, error) {
	if err := checkInstanceJSON(inst); err != nil {
		return nil, err
	}
	w := wireInstance{
		V: instanceFormat, Store: inst.Store, Token: inst.Token,
		C:     wireComponent{Path: inst.Path, Name: inst.Name, ID: inst.ID, Page: inst.PagePath, Locale: inst.Locale, Key: inst.Key, Route: inst.Route, Parts: inst.RouteParts},
		Props: markFloats(inst.Props), Ctx: markFloats(inst.Ctx), State: markFloats(inst.State), Keys: inst.Keys,
		Touch: inst.Touched, Sess: inst.Session, Ver: inst.Version, Slots: inst.Slots,
	}
	if w.State == nil {
		w.State = map[string]any{}
	}
	for _, p := range inst.Pending {
		w.Pend = append(w.Pend, wirePending{ID: p.ID, Field: p.Field, Iat: p.Issued.UTC().Format(time.RFC3339Nano)})
	}
	if !inst.Issued.IsZero() {
		w.Iat = inst.Issued.UTC().Format(time.RFC3339Nano)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// State text is never HTML-escaped here: the bytes are a store's value,
	// not markup, and escaping would change them across encoders.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(w); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// DecodeInstance reads EncodeInstance's output. Integers come back as
// int64 and other numbers as float64 (the action endpoint's rule), so a
// round trip never turns 3 into 3.0. An unknown format version is
// errUnknownFormat, which every built-in store reports as "not found".
func DecodeInstance(b []byte) (*Instance, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var w wireInstance
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("brick: decode state: %w", err)
	}
	if w.V != instanceFormat {
		return nil, fmt.Errorf("%w %d", errUnknownFormat, w.V)
	}
	inst := &Instance{
		Store: w.Store, Token: w.Token,
		Path: w.C.Path, Name: w.C.Name, ID: w.C.ID, PagePath: w.C.Page, Locale: w.C.Locale, Key: w.C.Key, Route: w.C.Route, RouteParts: w.C.Parts,
		Props: normalizeMap(w.Props), Ctx: normalizeMap(w.Ctx), State: normalizeMap(w.State),
		Keys: w.Keys, Touched: w.Touch, Session: w.Sess, Version: w.Ver, Slots: w.Slots,
	}
	if inst.State == nil {
		inst.State = map[string]any{}
	}
	for _, p := range w.Pend {
		t, err := time.Parse(time.RFC3339Nano, p.Iat)
		if err != nil {
			return nil, fmt.Errorf("brick: decode state: pending: %w", err)
		}
		inst.Pending = append(inst.Pending, PendingUpload{ID: p.ID, Field: p.Field, Issued: t})
	}
	if w.Iat != "" {
		t, err := time.Parse(time.RFC3339Nano, w.Iat)
		if err != nil {
			return nil, fmt.Errorf("brick: decode state: iat: %w", err)
		}
		inst.Issued = t
	}
	return inst, nil
}

// markFloats returns m with every float64 written so it reads back as a
// float: encoding/json writes 3.0 as "3", which would decode as the int 3.
// Values that are not floats, maps or lists are kept as they are. Only the
// maps and lists on the way to a float are copied; the rest is shared with
// the instance, which is safe because the result is only ever encoded, and
// it saves copying a whole state on every save (most states hold no float).
func markFloats(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := markFloat(m)
	return out.(map[string]any)
}

// markFloat returns v marked, and whether that differs from v. A nil
// map[string]any or []any counts as changed: it is written as {} or [],
// never null, as the full copy this replaced always wrote it.
func markFloat(v any) (any, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return x, false // refused by the encoder with its own message
		}
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEn") {
			s += ".0"
		}
		return json.Number(s), true
	case map[string]any:
		if x == nil {
			return map[string]any{}, true
		}
		var out map[string]any
		for k, e := range x {
			if m, changed := markFloat(e); changed {
				if out == nil {
					out = maps.Clone(x)
				}
				out[k] = m
			}
		}
		if out == nil {
			return x, false
		}
		return out, true
	case []any:
		if x == nil {
			return []any{}, true
		}
		var out []any
		for i, e := range x {
			if m, changed := markFloat(e); changed {
				if out == nil {
					out = slices.Clone(x)
				}
				out[i] = m
			}
		}
		if out == nil {
			return x, false
		}
		return out, true
	}
	return v, false
}

func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := normalizeJSON(m).(map[string]any)
	return out
}

// checkInstanceJSON reports the first value in an instance's props, ctx or
// state that JSON cannot carry faithfully, by its path
// ("state.joined: time.Time is not JSON-compatible; store it as a string").
func checkInstanceJSON(inst *Instance) error {
	for _, part := range []struct {
		name string
		m    map[string]any
	}{{"props", inst.Props}, {"ctx", inst.Ctx}, {"state", inst.State}} {
		if err := checkJSONValue(part.name, part.m); err != nil {
			return err
		}
	}
	return nil
}

// checkJSONValue walks v. Allowed: nil, bool, string, every integer and
// float kind, and slices, arrays and string-keyed maps of those. Anything
// else (a struct, a time.Time, a func, a set from a script) would not come
// back as it went in, so it is refused here rather than corrupted later.
//
// It runs on every save and every reply, and nearly always passes, so it
// first asks jsonValueOK, which allocates nothing; only a refused value
// pays for the walk that builds paths and finds the first bad one in key
// order (jsonValuePath).
func checkJSONValue(path string, v any) error {
	if jsonValueOK(v) {
		return nil
	}
	return jsonValuePath(path, v)
}

// jsonValueOK accepts exactly what jsonValuePath accepts, without building
// a path or sorting keys. The two must change together;
// TestCheckJSONValueFastPath holds them to the same answers.
func jsonValueOK(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string, bool, json.Number:
		return true
	case float64:
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	case int, int64:
		return true
	case map[string]any:
		for _, e := range x {
			if !jsonValueOK(e) {
				return false
			}
		}
		return true
	case []any:
		for _, e := range x {
			if !jsonValueOK(e) {
				return false
			}
		}
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool, reflect.String:
		return rv.Type().PkgPath() == ""
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if !jsonValueOK(rv.Index(i).Interface()) {
				return false
			}
		}
		return true
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return false
		}
		iter := rv.MapRange()
		for iter.Next() {
			if !jsonValueOK(iter.Value().Interface()) {
				return false
			}
		}
		return true
	}
	return false
}

// jsonValuePath is checkJSONValue's slow walk: the first value JSON cannot
// carry, in key order, as an error naming its path.
func jsonValuePath(path string, v any) error {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case string, bool, json.Number:
		return nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("%w: %s: %v has no JSON form", ErrStateValue, path, x)
		}
		return nil
	case map[string]any:
		for _, k := range sortedMapKeys(x) {
			if err := jsonValuePath(path+"."+k, x[k]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for i, e := range x {
			if err := jsonValuePath(fmt.Sprintf("%s[%d]", path, i), e); err != nil {
				return err
			}
		}
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool, reflect.String:
		if rv.Type().PkgPath() == "" {
			return nil
		}
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if err := jsonValuePath(fmt.Sprintf("%s[%d]", path, i), rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			iter := rv.MapRange()
			for iter.Next() {
				if err := jsonValuePath(path+"."+iter.Key().String(), iter.Value().Interface()); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return fmt.Errorf("%w: %s: %T is not JSON-compatible; store it as a string, number, list or dict", ErrStateValue, path, v)
}

func sortedMapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// BytesStore is a place that keeps bytes under a key — the piece a
// database adapter implements. Wrap it with Bytes to
// get a StateStore; the wrapper makes the keys and runs the codec (and an
// optional sealer). session is the hashed session id, passed so an adapter
// can index it for RevokeSession; it is never the raw id.
type BytesStore interface {
	Load(ctx context.Context, key string) ([]byte, bool, error)
	Save(ctx context.Context, key string, data []byte, ttl time.Duration, session string) error
	Remove(ctx context.Context, key string) error
}

// BytesLister is the listing upgrade of a BytesStore: it walks every live
// key and its bytes; fn returning false stops. Bytes turns it into Lister.
type BytesLister interface {
	EachBytes(ctx context.Context, fn func(key string, data []byte) bool) error
}

// BytesOptions configures Bytes.
type BytesOptions struct {
	// TTL is how long an instance lives after its last save; zero means
	// DefaultStateTTL.
	TTL time.Duration
	// Codec encodes instances; nil means Options.Codec, and failing that
	// JSONCodec.
	Codec Codec
	// Sealer, when set, encrypts what the store holds (a shared Redis, a
	// compliance rule), binding each value to its key.
	Sealer Sealer
	// Rand makes the handles; nil means crypto/rand (others for tests).
	Rand io.Reader
}

// BytesStates is a StateStore over a BytesStore (see Bytes). It is
// Revoker, Lister and Pruner when the BytesStore underneath is Revoker,
// BytesLister and Pruner; otherwise those methods report
// errors.ErrUnsupported, which Engine.RevokeSession and statetest skip.
// Likewise WatchEnds reports the reasons of a BytesEndWatcher underneath,
// and none otherwise.
type BytesStates struct {
	store BytesStore
	opts  BytesOptions
}

// Bytes wraps a BytesStore as a StateStore. Handles are random 128-bit
// hex keys, made on the first Put and kept on every later one.
func Bytes(store BytesStore, opts BytesOptions) *BytesStates {
	if opts.TTL <= 0 {
		opts.TTL = DefaultStateTTL
	}
	return &BytesStates{store: store, opts: opts}
}

// withCodec returns a copy using c when none was set (Options.Codec).
func (b *BytesStates) withCodec(c Codec) *BytesStates {
	if b.opts.Codec != nil || c == nil {
		return b
	}
	cp := *b
	cp.opts.Codec = c
	return &cp
}

func (b *BytesStates) codec() Codec {
	if b.opts.Codec != nil {
		return b.opts.Codec
	}
	return JSONCodec{}
}

// bytesAD binds an encrypted-at-rest value to its key, so values cannot be
// swapped between keys inside the store.
func bytesAD(key string) []byte { return []byte("brick-bytes|" + key) }

// Put encodes (and seals) inst and saves it, returning its key.
func (b *BytesStates) Put(ctx context.Context, inst *Instance) (string, error) {
	data, err := b.codec().Encode(inst)
	if err != nil {
		return "", err
	}
	key := inst.Handle
	if key == "" {
		if key, err = randomHex(b.opts.Rand); err != nil {
			return "", err
		}
	}
	if b.opts.Sealer != nil {
		if data, err = b.opts.Sealer.Seal(ctx, data, bytesAD(key)); err != nil {
			return "", err
		}
	}
	if err := b.store.Save(ctx, key, data, b.opts.TTL, inst.Session); err != nil {
		return "", err
	}
	return key, nil
}

// Get loads, opens and decodes. A value that does not open or decode (an
// unknown format version, a codec changed in a deploy, a retired key) is
// "not found", so the component recovers in place
// instead of failing every action.
func (b *BytesStates) Get(ctx context.Context, handle string) (*Instance, bool, error) {
	data, ok, err := b.store.Load(ctx, handle)
	if err != nil || !ok {
		return nil, false, err
	}
	inst := b.decode(ctx, handle, data)
	if inst == nil {
		return nil, false, nil
	}
	return inst, true, nil
}

func (b *BytesStates) decode(ctx context.Context, key string, data []byte) *Instance {
	if b.opts.Sealer != nil {
		plain, err := b.opts.Sealer.Open(ctx, data, bytesAD(key))
		if err != nil {
			return nil
		}
		data = plain
	}
	inst, err := b.codec().Decode(data)
	if err != nil {
		return nil
	}
	inst.Handle = key
	return inst
}

// Delete removes the key.
func (b *BytesStates) Delete(ctx context.Context, handle string) error {
	return b.store.Remove(ctx, handle)
}

// RevokeSession passes through to a BytesStore that is a Revoker.
func (b *BytesStates) RevokeSession(ctx context.Context, session string) error {
	if r, ok := b.store.(Revoker); ok {
		return r.RevokeSession(ctx, session)
	}
	return errors.ErrUnsupported
}

// Each decodes every instance of a BytesStore that is a BytesLister;
// values that do not decode are skipped.
func (b *BytesStates) Each(ctx context.Context, fn func(*Instance) bool) error {
	l, ok := b.store.(BytesLister)
	if !ok {
		return errors.ErrUnsupported
	}
	return l.EachBytes(ctx, func(key string, data []byte) bool {
		if inst := b.decode(ctx, key, data); inst != nil {
			return fn(inst)
		}
		return true
	})
}

// WatchEnds passes through to a BytesStore that is a BytesEndWatcher,
// opening and decoding each ended value with this wrapper's sealer and
// codec; a value that no longer opens (a retired key) is not reported.
// Over any other BytesStore it reports no reasons, so the engine tells the
// host that OnInstanceEnd never fires for this store.
func (b *BytesStates) WatchEnds(fn EndFunc) []EndReason {
	w, ok := b.store.(BytesEndWatcher)
	if !ok {
		return nil
	}
	if fn == nil {
		return w.WatchEndsBytes(nil)
	}
	return w.WatchEndsBytes(func(ctx context.Context, key string, data []byte, reason EndReason) {
		if inst := b.decode(ctx, key, data); inst != nil {
			fn(ctx, inst, reason)
		}
	})
}

// Prune passes through to a BytesStore that is a Pruner.
func (b *BytesStates) Prune(ctx context.Context, now time.Time) (int, error) {
	if p, ok := b.store.(Pruner); ok {
		return p.Prune(ctx, now)
	}
	return 0, errors.ErrUnsupported
}
