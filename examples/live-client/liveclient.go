// Package liveclient is the part of the browser runtime the live-*
// examples need to show their wire traffic from Go: read the stateful
// components off a page, post an action the way runtime.js does, and open
// a page's push stream. It exists so each example's -demo and its test
// print real requests and real answers, not a description of them.
//
// It is a teaching aid, not a runtime: it does not morph, queue, retry or
// run effects. Each example says which effects the real runtime would run
// at the point the transcript shows them.
package liveclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ubgo/brick"
)

// actionHeaderName and actionHeaderValue mark a request as an action. A
// cross-site form cannot set a custom header, so the engine refuses an
// action without it (state.go actionHeader).
const (
	actionHeaderName  = "Brick-Action"
	actionHeaderValue = "1"
)

// Client talks to one running app, holding its own cookies the way one
// browser does: two Clients are two people.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the app at base (an httptest.Server's URL).
func New(base string) *Client {
	jar, _ := cookiejar.New(nil) // cookiejar.New never fails with nil options
	return &Client{base: strings.TrimSuffix(base, "/"), http: &http.Client{Jar: jar}}
}

// SetCookie stores a cookie for every later request, as a sign-in would.
func (c *Client) SetCookie(name, value string) error {
	u, err := url.Parse(c.base)
	if err != nil {
		return err
	}
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
	return nil
}

// Get fetches a page and returns its body; any status but 200 is an error.
func (c *Client) Get(path string) (string, error) {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, b)
	}
	return string(b), nil
}

// Post sends an empty POST, as a webhook or a form would, and returns
// the status.
func (c *Client) Post(path string) (int, error) {
	resp, err := c.http.Post(c.base+path, "application/x-www-form-urlencoded", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, err
}

// Root is one stateful component as its root element carries it: the
// file's stem, and the handle and token an action must send back.
type Root struct {
	Name   string
	Handle string
	Token  string
}

// rootRE reads a stateful root's attributes in the order the engine
// writes them: data-brick-state, data-brick-token, data-brick-name.
var rootRE = regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([^"]+)" data-brick-name="([^"]+)"`)

// Roots lists the stateful components in page, in document order.
func Roots(page string) []*Root {
	var out []*Root
	for _, m := range rootRE.FindAllStringSubmatch(page, -1) {
		out = append(out, &Root{Handle: m[1], Token: m[2], Name: m[3]})
	}
	return out
}

// Find returns the first root named name, or an error naming what was there.
func Find(page, name string) (*Root, error) {
	var seen []string
	for _, r := range Roots(page) {
		if r.Name == name {
			return r, nil
		}
		seen = append(seen, r.Name)
	}
	return nil, fmt.Errorf("no stateful <%s> on the page (found %v)", name, seen)
}

// Answer is an action's reply: the HTTP status, and for a 200 the envelope
// the runtime asks for with Accept: brick.EffectsContentType.
type Answer struct {
	Status  int
	HTML    string
	Effects []json.RawMessage
	// Body is the raw reply when the status is not 200 (the error text).
	Body string
}

// envelope is the JSON an action answers with (action.go actionEnvelope).
type envelope struct {
	HTML    string            `json:"html"`
	Effects []json.RawMessage `json:"effects"`
}

// Act posts one action to root, as runtime.js does: a JSON POST with the
// Brick-Action header, asking for the effects envelope. On success it
// moves root to the handle and token of the new render, as the morph does.
func (c *Client) Act(root *Root, action string, args ...any) (*Answer, error) {
	b, status, err := c.post(root, action, args, brick.EffectsContentType)
	if err != nil {
		return nil, err
	}
	return answer(root, action, status, b)
}

// ActStream posts an action that may stream (brick.stream): it asks for
// the NDJSON answer as the runtime does, and returns every chunk line
// before the final envelope, which it decodes like Act.
func (c *Client) ActStream(root *Root, action string, args ...any) (chunks []string, ans *Answer, err error) {
	b, status, err := c.post(root, action, args, brick.EffectsContentType+", "+brick.StreamContentType)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if status != http.StatusOK || len(lines) < 2 {
		ans, err := answer(root, action, status, b)
		return nil, ans, err
	}
	ans, err = answer(root, action, status, []byte(lines[len(lines)-1]))
	return lines[:len(lines)-1], ans, err
}

// post sends one action request and returns the raw answer.
func (c *Client) post(root *Root, action string, args []any, accept string) ([]byte, int, error) {
	if args == nil {
		args = []any{}
	}
	body, err := json.Marshal(brick.ActionRequest{Handle: root.Handle, Token: root.Token, Action: action, Args: args})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+brick.DefaultAssetBase+brick.ActionEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set(actionHeaderName, actionHeaderValue)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

// answer decodes one envelope (or keeps an error's text) and moves root
// to the new render's handle and token.
func answer(root *Root, action string, status int, b []byte) (*Answer, error) {
	ans := &Answer{Status: status}
	if status != http.StatusOK {
		ans.Body = strings.TrimSpace(string(b))
		return ans, nil
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("action %s: %w", action, err)
	}
	ans.HTML, ans.Effects = env.HTML, env.Effects
	if next := Roots(env.HTML); len(next) > 0 {
		root.Handle, root.Token = next[0].Handle, next[0].Token
	}
	return ans, nil
}

// EffectLines returns each effect as one line of compact JSON, exactly as
// the envelope carried it.
func (a *Answer) EffectLines() []string {
	out := make([]string, 0, len(a.Effects))
	for _, ef := range a.Effects {
		var buf bytes.Buffer
		if err := json.Compact(&buf, ef); err != nil {
			out = append(out, string(ef))
			continue
		}
		out = append(out, buf.String())
	}
	return out
}

// engineAttrRE is one engine-owned attribute on a stateful root. Handles
// and tokens change on every render, so a transcript leaves them out.
var engineAttrRE = regexp.MustCompile(` data-brick(-[a-z-]+)?="[^"]*"`)

// spaceRE and tagGapRE collapse the whitespace a template's layout adds.
var (
	spaceRE  = regexp.MustCompile(`\s+`)
	tagGapRE = regexp.MustCompile(`>\s+<`)
)

// Tidy removes the engine's root attributes and the layout whitespace,
// so markup prints on one stable line.
func Tidy(markup string) string {
	s := engineAttrRE.ReplaceAllString(markup, "")
	s = tagGapRE.ReplaceAllString(s, "><")
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

// listenTokenRE reads the push token off a listening root.
var listenTokenRE = regexp.MustCompile(`data-brick-listen-token="([^"]+)"`)

// Stream is an open push stream: the server-sent events of one page.
type Stream struct {
	body   io.ReadCloser
	events chan string
}

// Listen opens page's push stream the way the runtime does: one
// EventSource on /_brick/events carrying every listening root's token.
func (c *Client) Listen(page string) (*Stream, error) {
	var tokens []string
	for _, m := range listenTokenRE.FindAllStringSubmatch(page, -1) {
		tokens = append(tokens, html.UnescapeString(m[1]))
	}
	if len(tokens) == 0 {
		return nil, errors.New("no listening component on the page")
	}
	resp, err := c.http.Get(c.base + brick.DefaultAssetBase + brick.EventsEndpoint + "?" + url.Values{"l": tokens}.Encode())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("events: %d", resp.StatusCode)
	}
	s := &Stream{body: resp.Body, events: make(chan string, 16)}
	go s.read()
	return s, nil
}

// read turns "event: refresh" + "data: <topic>" pairs into topics on
// s.events, and closes it when the stream ends.
func (s *Stream) read() {
	defer close(s.events)
	sc := bufio.NewScanner(s.body)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		if e, ok := strings.CutPrefix(line, "event: "); ok {
			event = e
			continue
		}
		if d, ok := strings.CutPrefix(line, "data: "); ok && event == brick.EventRefresh {
			s.events <- d
			event = ""
		}
	}
}

// Next waits up to within for the next refresh event and returns its
// topic. A demo must never hang, so a quiet stream is an error.
func (s *Stream) Next(within time.Duration) (string, error) {
	select {
	case topic, ok := <-s.events:
		if !ok {
			return "", errors.New("the stream ended")
		}
		return topic, nil
	case <-time.After(within):
		return "", fmt.Errorf("no event within %s", within)
	}
}

// Close ends the stream, as leaving the page does.
func (s *Stream) Close() error { return s.body.Close() }

// Transcript prints each action it sends and the effects its answer
// carried, one per line in the order the browser runs them, so every
// example's -demo reads the same way.
type Transcript struct {
	W io.Writer
	C *Client
}

// call renders an action call the way a brick:click attribute writes it.
func call(root *Root, action string, args []any) (string, error) {
	shown, err := json.Marshal(append([]any{}, args...))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s(%s)", root.Name, action, strings.TrimSuffix(strings.TrimPrefix(string(shown), "["), "]")), nil
}

// Act posts the action, prints it and its effects, and fails on any
// status but 200.
func (t Transcript) Act(root *Root, action string, args ...any) (*Answer, error) {
	line, err := call(root, action, args)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(t.W, "POST %s\n", line)
	ans, err := t.C.Act(root, action, args...)
	if err != nil {
		return nil, err
	}
	if ans.Status != http.StatusOK {
		return nil, fmt.Errorf("%s: %d %s", line, ans.Status, ans.Body)
	}
	for _, ef := range ans.EffectLines() {
		fmt.Fprintf(t.W, "  effect %s\n", ef)
	}
	return ans, nil
}

// ActStream is Act for a streaming action: it prints each chunk line
// before the effects.
func (t Transcript) ActStream(root *Root, action string, args ...any) (*Answer, error) {
	line, err := call(root, action, args)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(t.W, "POST %s  (Accept: %s)\n", line, brick.StreamContentType)
	chunks, ans, err := t.C.ActStream(root, action, args...)
	if err != nil {
		return nil, err
	}
	if ans.Status != http.StatusOK {
		return nil, fmt.Errorf("%s: %d %s", line, ans.Status, ans.Body)
	}
	for _, ch := range chunks {
		fmt.Fprintf(t.W, "  chunk %s\n", ch)
	}
	for _, ef := range ans.EffectLines() {
		fmt.Fprintf(t.W, "  effect %s\n", ef)
	}
	return ans, nil
}
