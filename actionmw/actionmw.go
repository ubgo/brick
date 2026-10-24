// Package actionmw holds the common brick action middleware: audit logging, rate limiting, session requirement, timeouts and
// panic recovery. Standard library only; each is an ordinary
// brick.ActionMiddleware, so an app mixes these with its own in
// brick.Options.ActionMiddleware, outermost first.
package actionmw

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ubgo/brick"
)

// Slog logs one line per action: component, action, store, duration and
// outcome. A refused or failed action logs at Warn with its error.
//
// A component's brick: {log:} setting (brick.ActionLog) tunes the success
// line: a level word logs it at that level, false drops it. A failure is
// logged at Warn whatever the component says, because a refused action (a
// rate limit, a forbidden token, a locked field) is a security signal, and
// letting one file silence it would let that file hide abuse.
func Slog(logger *slog.Logger) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			start := time.Now()
			res, err := next(ctx, a)
			attrs := []any{"component", a.Component, "action", a.Action, "store", a.Store, "took", time.Since(start)}
			switch {
			case err != nil:
				logger.WarnContext(ctx, "brick action failed", append(attrs, "err", err)...)
			case a.Log == nil:
				logger.InfoContext(ctx, "brick action", append(attrs, "version", res.Version)...)
			case !a.Log.Off:
				logger.Log(ctx, a.Log.Level, "brick action", append(attrs, "version", res.Version)...)
			}
			return res, err
		}
	}
}

// Limiter decides whether one more action under key may run now, and when
// to retry if not. A Redis-backed one shares the budget between servers.
type Limiter interface {
	Allow(ctx context.Context, key string) (ok bool, retryAfter time.Duration, err error)
}

// KeyFunc picks what a rate limit counts by.
type KeyFunc func(a *brick.ActionCall) string

// BySession counts per session, falling back to the client address for a
// request without one.
func BySession(a *brick.ActionCall) string {
	if a.Session != "" {
		return "s:" + a.Session
	}
	return ByIP(a)
}

// ByIP counts per client address (the connection's, not a forwarded
// header: a proxy's header is the host's policy to trust).
func ByIP(a *brick.ActionCall) string {
	if a.Request == nil {
		return "ip:"
	}
	host, _, err := net.SplitHostPort(a.Request.RemoteAddr)
	if err != nil {
		host = a.Request.RemoteAddr
	}
	return "ip:" + host
}

// RateLimit refuses an action over the limiter's budget with 429 and
// Retry-After. A limiter error refuses with 503 rather than letting the
// action through, so an outage never silently removes the limit.
func RateLimit(l Limiter, key KeyFunc) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			ok, retry, err := l.Allow(ctx, key(a))
			if err != nil {
				return nil, &brick.ActionError{Status: http.StatusServiceUnavailable, Message: "rate limiter unavailable", RetryAfter: time.Second, Err: err}
			}
			if !ok {
				return nil, &brick.ActionError{Status: http.StatusTooManyRequests, Message: "too many actions; slow down", RetryAfter: retry}
			}
			return next(ctx, a)
		}
	}
}

// MemoryLimiter is an in-process token bucket per key: n actions per
// period, refilled continuously. Buckets idle for a full period are
// dropped on the next call, so memory stays bounded by active keys.
func MemoryLimiter(n int, per time.Duration) Limiter {
	return &memLimiter{rate: float64(n) / per.Seconds(), burst: float64(n), per: per, buckets: map[string]*bucket{}, now: time.Now}
}

type memLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	per     time.Duration
	buckets map[string]*bucket
	swept   time.Time
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func (m *memLimiter) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if now.Sub(m.swept) > m.per {
		for k, b := range m.buckets {
			if now.Sub(b.last) > m.per {
				delete(m.buckets, k)
			}
		}
		m.swept = now
	}
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: m.burst, last: now}
		m.buckets[key] = b
	}
	b.tokens = math.Min(m.burst, b.tokens+now.Sub(b.last).Seconds()*m.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0, nil
	}
	return false, time.Duration((1 - b.tokens) / m.rate * float64(time.Second)), nil
}

// RequireSession refuses actions with no session (403), for sites where
// every stateful component belongs to a signed-in user.
func RequireSession() brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			if a.Session == "" {
				return nil, brick.ErrForbidden
			}
			return next(ctx, a)
		}
	}
}

// Timeout bounds an action (script, store, re-render) by d through its
// context; a script or store that honours the context stops.
func Timeout(d time.Duration) brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (*brick.ActionResult, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			res, err := next(ctx, a)
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, &brick.ActionError{Status: http.StatusGatewayTimeout, Message: "the action took too long", Err: err}
			}
			return res, err
		}
	}
}

// Recover turns a panic below it (a middleware, a host function) into an
// error answered 422, never a dead process. Put it outermost.
func Recover() brick.ActionMiddleware {
	return func(next brick.ActionFunc) brick.ActionFunc {
		return func(ctx context.Context, a *brick.ActionCall) (res *brick.ActionResult, err error) {
			defer func() {
				if p := recover(); p != nil {
					res, err = nil, fmt.Errorf("brick: action %s on <%s> panicked: %v", a.Action, a.Component, p)
				}
			}()
			return next(ctx, a)
		}
	}
}
