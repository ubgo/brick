package brick

import (
	"errors"
	"fmt"
	"html/template"
	"slices"
	"strings"
	texttemplate "text/template"
)

// brick: {listen:}: the topics a stateful component refreshes
// on. Three spellings, the shortest first:
//
//	brick: {listen: orders}
//	brick: {listen: [orders, "order:{{ .Props.id }}"]}
//	brick: {listen: {topics: [orders], keep_alive: true}}
//	brick: {listen: {presence: "room:{{ .Props.id }}"}}
//
// A topic may use {{ .Props.x }}, {{ .State.x }}, {{ .Params.x }} and
// {{ .Key }}, evaluated each time the component renders; what it renders
// to must be a valid topic (ValidTopic), or the render fails. A literal
// topic is checked at compile. keep_alive refreshes while the tab is
// hidden, like brick:poll.keep-alive; without it a hidden tab defers the
// refresh until it is shown. presence (map spelling only, one topic or a
// list) names the topics whose viewers the component shows with
// {{ .Presence "topic" }} (presence.go); it refreshes whenever one of them
// changes. topics may be left out when presence is given.
const (
	listenKeyTopics    = "topics"
	listenKeyKeepAlive = "keep_alive"
	listenKeyPresence  = "presence"
	// maxListenDecl bounds the topics one component declares.
	maxListenDecl = 16
)

// listenKeys are the keys of the map spelling.
var listenKeys = []string{listenKeyTopics, listenKeyKeepAlive, listenKeyPresence}

// listenDecl is a parsed brick: {listen:}.
type listenDecl struct {
	// topics are the templates as written; tmpls the parsed ones, nil for a
	// literal topic.
	topics    []string
	tmpls     []*texttemplate.Template
	keepAlive bool
	// presence and ptmpls are the presence topics, as topics and tmpls.
	presence []string
	ptmpls   []*texttemplate.Template
}

// listenData is what a topic template sees.
type listenData struct {
	Props  any
	State  map[string]any
	Params map[string]any
	Key    any
}

var errListenShape = errors.New("brick: listen: must be a topic, a list of topics, or {topics: [...], keep_alive: true, presence: [...]}")

// topicList reads a topic or a list of topics.
func topicList(raw any) ([]string, error) {
	switch t := raw.(type) {
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s, ok := x.(string)
			if !ok {
				return nil, errListenShape
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, errListenShape
	}
}

// parseTopicTemplates parses each topic that uses {{ }}, checking a
// literal one with valid; nil marks a literal.
func parseTopicTemplates(topics []string, valid func(string) bool, maxLen int) ([]*texttemplate.Template, error) {
	tmpls := make([]*texttemplate.Template, 0, len(topics))
	for _, s := range topics {
		if !strings.Contains(s, "{{") {
			if !valid(s) {
				return nil, fmt.Errorf("brick: listen: %q is not a topic (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", s, maxLen)
			}
			tmpls = append(tmpls, nil)
			continue
		}
		tp, err := texttemplate.New("listen").Option("missingkey=error").Parse(s)
		if err != nil {
			return nil, fmt.Errorf("brick: listen: %q: %w", s, err)
		}
		tmpls = append(tmpls, tp)
	}
	return tmpls, nil
}

// parseListen reads the three spellings and parses each topic template.
func parseListen(v any) (*listenDecl, error) {
	d := &listenDecl{}
	raw := v
	if mp, ok := v.(map[string]any); ok {
		for k := range mp {
			if !contains(listenKeys, k) {
				return nil, fmt.Errorf("brick: listen: unknown key %q (known: %s)", k, strings.Join(listenKeys, ", "))
			}
		}
		if ka, ok := mp[listenKeyKeepAlive]; ok {
			b, ok := ka.(bool)
			if !ok {
				return nil, errors.New("brick: listen: keep_alive must be true or false")
			}
			d.keepAlive = b
		}
		raw = mp[listenKeyTopics]
		if pr, ok := mp[listenKeyPresence]; ok {
			ps, err := topicList(pr)
			if err != nil {
				return nil, err
			}
			if len(ps) == 0 {
				return nil, errors.New("brick: listen: presence names no topic")
			}
			d.presence = ps
		}
	}
	if raw != nil || d.presence == nil {
		ts, err := topicList(raw)
		if err != nil {
			return nil, err
		}
		d.topics = ts
	}
	if len(d.topics) == 0 && len(d.presence) == 0 {
		return nil, errors.New("brick: listen: names no topic")
	}
	if n := len(d.topics) + len(d.presence); n > maxListenDecl {
		return nil, fmt.Errorf("brick: listen: %d topics, over the %d one component may declare", n, maxListenDecl)
	}
	var err error
	if d.tmpls, err = parseTopicTemplates(d.topics, ValidTopic, MaxTopicLen); err != nil {
		return nil, err
	}
	if d.ptmpls, err = parseTopicTemplates(d.presence, ValidPresenceTopic, MaxPresenceTopicLen); err != nil {
		return nil, err
	}
	return d, nil
}

// checkListen is the compile check of brick: {listen:}: it needs state:
// (a refresh is an action) and a configured Push.
func checkListen(src []byte, m *meta, have bool) (int, error) {
	if m.listen == nil {
		return 0, nil
	}
	line := brickLine(src, brickListen)
	if m.state == nil {
		return line, errors.New("brick: listen: needs state: — a pushed topic refreshes the component, which is an action")
	}
	if !have {
		return line, errors.New("brick: listen: server push is off (Options.Push); set one, such as brick.NewPush(brick.PushOptions{}), so a listening component never silently stops updating")
	}
	return 0, nil
}

// listenAttrs writes a listening root's topics and its listen token, sealed
// for the render's session.
func listenAttrs(s *Scope) (string, error) {
	d := s.file.meta.listen
	if d == nil || s.st == nil || s.st.e.opts.Push == nil {
		return "", nil
	}
	topics, err := renderTopics(s, d.topics, d.tmpls, ValidTopic, MaxTopicLen)
	if err != nil {
		return "", err
	}
	presence, err := s.presenceTopics()
	if err != nil {
		return "", err
	}
	if len(presence) > 0 && s.st.e.opts.Push.opts.Presence.Store == nil {
		return "", errPresenceStore
	}
	tok, err := s.st.e.opts.Push.listenTokenWith(s.st.ctx, topics, presence, hashSession(s.st.session))
	if err != nil {
		return "", err
	}
	// A presence topic is heard as its control topic, so the runtime
	// refreshes this root on a join or leave with no code of its own.
	names := slices.Clone(topics)
	for _, p := range presence {
		names = append(names, presenceControl(p))
	}
	// Topics are [A-Za-z0-9_.:/@-] and the token base64url: neither needs
	// escaping, and HTMLEscapeString keeps that true if either changes.
	out := ` data-brick-listen="` + template.HTMLEscapeString(strings.Join(names, " ")) + `" data-brick-listen-token="` + template.HTMLEscapeString(tok) + `"`
	if d.keepAlive {
		out += ` data-brick-listen-keep`
	}
	return out, nil
}

// renderTopics evaluates topic templates against the scope, checks each
// result with valid and drops duplicates.
func renderTopics(s *Scope, raw []string, tmpls []*texttemplate.Template, valid func(string) bool, maxLen int) ([]string, error) {
	data := listenData{Props: s.Props, State: s.State, Params: s.Params, Key: s.Key}
	out := make([]string, 0, len(raw))
	for i, t := range raw {
		if tp := tmpls[i]; tp != nil {
			var b strings.Builder
			if err := tp.Execute(&b, data); err != nil {
				return nil, fmt.Errorf("brick: listen: %q: %w", t, err)
			}
			t = b.String()
			if !valid(t) {
				return nil, fmt.Errorf("brick: listen: %q rendered %q, which is not a topic (1-%d characters from A-Z a-z 0-9 _ . : / @ -)", raw[i], t, maxLen)
			}
		}
		if !contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}

// presenceTopics are the component's presence topics as rendered now.
func (s *Scope) presenceTopics() ([]string, error) {
	d := s.file.meta.listen
	if d == nil || len(d.presence) == 0 {
		return nil, nil
	}
	return renderTopics(s, d.presence, d.ptmpls, ValidPresenceTopic, MaxPresenceTopicLen)
}
