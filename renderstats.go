package brick

import (
	"cmp"
	"slices"
)

// RenderStats is what one page render did, counted while it ran: the Dev
// toolbar's numbers. Every field is a count of work, never a
// duration, so the stats of a render are as deterministic as its bytes
// (E6) and a cached Result carries no clock reading. A host times a
// render itself, around Render or Fill.
type RenderStats struct {
	// Components is how many component calls rendered, including islands
	// captured for a later Fill (their output renders then, uncounted).
	Components int
	// Types counts the calls per component name, sorted by count, highest
	// first, ties by name.
	Types []TypeCount
	// Islands is how many islands the page holds; LazyIslands is how many
	// of them are brick:lazy, fetched by the browser after load.
	Islands     int
	LazyIslands int
	// LoaderCalls is how many times a load: function ran (a memoised
	// repeat is not a call); BatchCalls is how many times a load_many
	// function ran, and BatchItems how many inputs those calls answered.
	LoaderCalls int
	BatchCalls  int
	BatchItems  int
}

// WithStats makes Fill add what it renders to s: each island's components
// and loader calls, which the page render (Result.Stats) only captured. A
// host passes a copy of Result.Stats, since a cached Result is shared;
// Fill never changes the Result itself. Lazy islands render on their own
// request and are not counted here.
func WithStats(s *RenderStats) RenderOption {
	return func(o *renderOptions) { o.stats = s }
}

// TypeCount is one component name and how many times it rendered.
type TypeCount struct {
	Name  string
	Count int
}

// Top returns the first n entries of Types: the most rendered components.
func (s RenderStats) Top(n int) []TypeCount {
	if n < len(s.Types) {
		return s.Types[:n]
	}
	return s.Types
}

// renderCounts is the counting side of RenderStats, a field of the render
// state. Its zero value counts.
type renderCounts struct {
	types   map[string]int
	calls   int
	loaders int
	batches int
	items   int
}

// component counts one rendered call of name.
func (c *renderCounts) component(name string) {
	if c.types == nil {
		c.types = map[string]int{}
	}
	c.types[name]++
	c.calls++
}

// loader counts one load: call.
func (c *renderCounts) loader() { c.loaders++ }

// batch counts one load_many call answering n inputs.
func (c *renderCounts) batch(n int) {
	c.batches++
	c.items += n
}

// merge adds an island render's counts. root is the island's component,
// already counted once when the page captured the island, so its second
// count (the render of its root) is dropped. Types is rebuilt, never
// written in place, so a slice shared with a cached Result is untouched.
func (s *RenderStats) merge(c *renderCounts, root string) {
	byName := map[string]int{}
	for _, t := range s.Types {
		byName[t.Name] = t.Count
	}
	calls := c.calls
	for name, n := range c.types {
		if name == root {
			n--
			calls--
		}
		byName[name] += n
	}
	s.Components += calls
	s.LoaderCalls += c.loaders
	s.BatchCalls += c.batches
	s.BatchItems += c.items
	s.Types = sortedCounts(byName)
}

// sortedCounts lists name counts, highest first, ties by name; a zero
// count is left out.
func sortedCounts(byName map[string]int) []TypeCount {
	var out []TypeCount
	for name, n := range byName {
		if n > 0 {
			out = append(out, TypeCount{Name: name, Count: n})
		}
	}
	slices.SortFunc(out, func(a, b TypeCount) int {
		if d := cmp.Compare(b.Count, a.Count); d != 0 {
			return d
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// stats builds the RenderStats of a finished render; islands are counted
// from its chunks, where they are recorded already.
func (c *renderCounts) stats(chunks []Chunk) RenderStats {
	s := RenderStats{Components: c.calls, Types: sortedCounts(c.types), LoaderCalls: c.loaders, BatchCalls: c.batches, BatchItems: c.items}
	for _, ch := range chunks {
		if ch.Kind == ChunkIsland {
			s.Islands++
			if ch.Island != nil && ch.Island.Lazy != "" {
				s.LazyIslands++
			}
		}
	}
	return s
}
