package site

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ubgo/brick"
)

// Response headers for pages that carry a stateful component.
const (
	// DefaultStateCacheControl keeps a response holding a handle or sealed
	// blob out of every shared cache: it was made for one request (and one
	// session), and a CDN serving it to someone else would show them the
	// HTML it rendered.
	DefaultStateCacheControl = "private, no-cache"
)

// DefaultPruneEvery is how often a served site prunes stores that need it.
const DefaultPruneEvery = 10 * time.Minute

// ErrExportNeedsKeys is returned by Export for a page with a stateful
// component when the site's state does not survive this process: the
// exported page would carry a handle no running server will ever open, so
// every click would answer 410. Set Options.StateKeys to the
// keys the action server uses.
var ErrExportNeedsKeys = errors.New("site: a page has a stateful component, and exporting it needs Options.StateKeys shared with the action server")

// memoryWarning is logged once, on the first response with a stateful
// component, when the site keeps state in this process because it was
// given neither StateKeys nor a store.
const memoryWarning = "site: stateful components keep state in this process: a restart loses it (recovered in place) and several servers need sticky routing; set StateKeys to seal state into the page"

// defaultStates is the store a site uses when Engine.States names none
// : sealed into the page with StateKeys, otherwise this
// process's memory. It is made once per Site, so a rebuild keeps state.
func defaultStates(o Options) (brick.StateStore, error) {
	if o.StateKeys == nil {
		return brick.NewMemoryStates(0, 0), nil
	}
	return brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(o.StateKeys, nil)})
}

// defaultTickets is the lazy islands' ticket store when Engine.IslandTickets
// names none: sealed with StateKeys, otherwise this process's
// memory. It is made once per Site, like defaultStates, so a Dev rebuild
// keeps the tickets of pages already open.
func defaultTickets(o Options) (brick.IslandTickets, error) {
	if o.StateKeys == nil {
		return brick.NewIslandTickets(brick.NewMemoryStates(brick.DefaultIslandTicketMax, brick.DefaultIslandTicketTTL)), nil
	}
	st, err := brick.NewSealedStates(brick.SealedOptions{Sealer: brick.NewAESGCMSealer(o.StateKeys, nil), TTL: brick.DefaultIslandTicketTTL})
	if err != nil {
		return nil, err
	}
	return brick.NewIslandTickets(st), nil
}

// statesShared reports whether a stateful page's handles outlive this
// process, so Export may write them: a sealing site, or a host-given store
// other than the in-process memory one.
func (s *Site) statesShared() bool {
	if st := s.opts.Engine.States; st != nil {
		_, mem := st.(*brick.MemoryStates)
		return !mem
	}
	return s.opts.StateKeys != nil
}

// stateHeaders marks a response holding a stateful component as private
// , and logs the memory-store warning once.
func (s *Site) stateHeaders(w http.ResponseWriter, res *brick.Result) {
	if res == nil || (!res.HasState() && !res.HasLazy()) {
		return
	}
	cc := s.opts.StateCacheControl
	if cc == "" {
		cc = DefaultStateCacheControl
	}
	w.Header().Set("Cache-Control", cc)
	// The runtime's recovery GET carries
	// brick.RecoverHeader; the site answers it with the whole page, and
	// Vary keeps a cache from mixing the two kinds of request.
	w.Header().Add("Vary", brick.RecoverHeader)
	if res.HasState() && s.opts.Engine.States == nil && s.opts.StateKeys == nil {
		s.warnMemory.Do(func() { s.opts.Logger.Warn(memoryWarning) })
	}
}

// Prune deletes expired instances from every store of the site that needs
// it (brick.Pruner) and returns how many went. ListenAndServe calls it every
// PruneEvery; a host serving the site its own way calls it on its own timer.
func (s *Site) Prune(ctx context.Context) (int, error) {
	stores := []brick.StateStore{s.opts.Engine.States, s.states}
	for _, st := range s.opts.Engine.StateStores {
		stores = append(stores, st)
	}
	total := 0
	var errs []error
	for _, st := range stores {
		p, ok := st.(brick.Pruner)
		if !ok {
			continue
		}
		n, err := p.Prune(ctx, time.Now())
		// brick.Bytes is always a Pruner and reports ErrUnsupported over a
		// store that expires on its own (Redis); that is not a failure,
		// or such a site would log one every tick.
		if errors.Is(err, errors.ErrUnsupported) {
			continue
		}
		total += n
		errs = append(errs, err)
	}
	return total, errors.Join(errs...)
}

// pruneLoop runs Prune every PruneEvery until ctx ends, logging failures:
// a store that cannot prune now keeps its rows until the next tick.
func (s *Site) pruneLoop(ctx context.Context) {
	every := s.opts.PruneEvery
	if every < 0 {
		return
	}
	if every == 0 {
		every = DefaultPruneEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.Prune(ctx); err != nil {
				s.opts.Logger.Error("site: prune state", "err", err)
			} else if n > 0 {
				s.opts.Logger.Info("site: pruned state", "removed", n)
			}
		}
	}
}
