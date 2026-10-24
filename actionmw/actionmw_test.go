package actionmw

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick"
)

func ok(context.Context, *brick.ActionCall) (*brick.ActionResult, error) {
	return &brick.ActionResult{HTML: []byte("<p>ok</p>"), Version: 1}, nil
}

func call(session string) *brick.ActionCall {
	r := httptest.NewRequest(http.MethodPost, "/_brick/action", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	return &brick.ActionCall{Request: r, Component: "Counter", Action: "save", Store: "default", Session: session}
}

func TestMemoryLimiter(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	l := MemoryLimiter(2, time.Second).(*memLimiter)
	l.now = func() time.Time { return now }
	mw := RateLimit(l, BySession)(ok)
	ctx := context.Background()
	for i := range 2 {
		if _, err := mw(ctx, call("a")); err != nil {
			t.Fatalf("call %d within the budget: %v", i, err)
		}
	}
	_, err := mw(ctx, call("a"))
	var ae *brick.ActionError
	if !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests || ae.RetryAfter <= 0 {
		t.Fatalf("over the budget: %v", err)
	}
	if _, err := mw(ctx, call("b")); err != nil {
		t.Fatalf("another session has its own budget: %v", err)
	}
	now = now.Add(600 * time.Millisecond)
	if _, err := mw(ctx, call("a")); err != nil {
		t.Fatalf("a token refills over time: %v", err)
	}
	if BySession(call("")) != "ip:10.0.0.1" {
		t.Fatalf("no session falls back to the address: %q", BySession(call("")))
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(context.Context, string) (bool, time.Duration, error) {
	return false, 0, errors.New("redis down")
}

// BV: let a limiter error through to next and this fails.
func TestRateLimitFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := RateLimit(brokenLimiter{}, ByIP)(ok)(context.Background(), call(""))
	var ae *brick.ActionError
	if !errors.As(err, &ae) || ae.Status != http.StatusServiceUnavailable {
		t.Fatalf("a limiter outage must refuse, not allow: %v", err)
	}
}

func TestRequireSession(t *testing.T) {
	t.Parallel()
	mw := RequireSession()(ok)
	if _, err := mw(context.Background(), call("")); !errors.Is(err, brick.ErrForbidden) {
		t.Fatalf("no session: %v", err)
	}
	if _, err := mw(context.Background(), call("s")); err != nil {
		t.Fatal(err)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	slow := func(ctx context.Context, _ *brick.ActionCall) (*brick.ActionResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := Timeout(10*time.Millisecond)(slow)(context.Background(), call(""))
	var ae *brick.ActionError
	if !errors.As(err, &ae) || ae.Status != http.StatusGatewayTimeout {
		t.Fatalf("timeout: %v", err)
	}
}

func TestRecover(t *testing.T) {
	t.Parallel()
	boom := func(context.Context, *brick.ActionCall) (*brick.ActionResult, error) { panic("boom") }
	_, err := Recover()(boom)(context.Background(), call(""))
	if err == nil || !strings.Contains(err.Error(), "panicked: boom") {
		t.Fatalf("recover: %v", err)
	}
}

func TestSlog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	_, _ = Slog(log)(ok)(context.Background(), call(""))
	_, _ = Slog(log)(RequireSession()(ok))(context.Background(), call(""))
	out := buf.String()
	if !strings.Contains(out, "component=Counter") || !strings.Contains(out, "action=save") || !strings.Contains(out, "level=WARN") {
		t.Fatalf("log: %s", out)
	}
}

// A component's brick: {log:} tunes the success line: a level moves it,
// false drops it, unset keeps Info. A failure is logged at Warn whatever
// the component says (G18), so log: false can never hide a refusal.
func TestSlogComponentLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, tc := range map[string]struct {
		log      *brick.ActionLog
		next     brick.ActionFunc
		minLevel slog.Level
		want     string
	}{
		"unset logs at info":         {nil, ok, slog.LevelInfo, "level=INFO msg=\"brick action\""},
		"debug logs at debug":        {&brick.ActionLog{Level: slog.LevelDebug}, ok, slog.LevelDebug, "level=DEBUG msg=\"brick action\""},
		"debug hidden by info":       {&brick.ActionLog{Level: slog.LevelDebug}, ok, slog.LevelInfo, ""},
		"warn logs at warn":          {&brick.ActionLog{Level: slog.LevelWarn}, ok, slog.LevelInfo, "level=WARN msg=\"brick action\""},
		"off drops success":          {&brick.ActionLog{Off: true}, ok, slog.LevelDebug, ""},
		"off still logs a refusal":   {&brick.ActionLog{Off: true}, RequireSession()(ok), slog.LevelDebug, "level=WARN msg=\"brick action failed\""},
		"debug still warns failures": {&brick.ActionLog{Level: slog.LevelDebug}, RequireSession()(ok), slog.LevelInfo, "level=WARN msg=\"brick action failed\""},
	} {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: tc.minLevel}))
		c := call("")
		c.Log = tc.log
		_, _ = Slog(log)(tc.next)(ctx, c)
		got := buf.String()
		if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: logged %q, want %q", name, got, tc.want)
		}
	}
}
