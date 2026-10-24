package brick

import (
	"maps"
	"sync"
)

// ComponentView is the data one component call rendered with: what a test asserts on when the output alone cannot tell whether
// a value reached the template (a prop the markup only branches on, a
// loader key used in an attribute).
type ComponentView struct {
	// Name is the component's tag name, Path its file (the caller's for
	// a local component).
	Name string
	Path string
	// Props are the props the template saw: the call site's, the loader's
	// merged under them, after declarations and defaults.
	Props map[string]any
	// Loaded are the keys the component's loader returned, nil when it
	// has no loader; a key the call site also gave shows the call site's
	// value in Props.
	Loaded map[string]any
}

// ViewLog records the components a render called, in call order, for a
// test (WithViewLog). It is safe for the concurrent island renders of
// one response. The maps of each view are copies; the values in them
// are the render's own, so a test must not change them.
type ViewLog struct {
	mu    sync.Mutex
	views []ComponentView
}

// Views returns every view recorded so far, oldest first.
func (l *ViewLog) Views() []ComponentView {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ComponentView(nil), l.views...)
}

// Reset forgets every view, so the next render's are alone in the log.
func (l *ViewLog) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.views = nil
}

func (l *ViewLog) record(name, path string, props, loaded map[string]any) {
	v := ComponentView{Name: name, Path: path, Props: maps.Clone(props), Loaded: maps.Clone(loaded)}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.views = append(l.views, v)
}

// WithViewLog records into l the data each component call of the render
// (and of Fill, an action's re-render, a lazy island's answer) rendered
// with. It is a testing seam: a render without it records nothing and
// pays one nil check per component call.
func WithViewLog(l *ViewLog) RenderOption {
	return func(o *renderOptions) { o.views = l }
}
