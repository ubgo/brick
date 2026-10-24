package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEngineOnlyApp(t *testing.T) {
	a, err := newApp("theme")
	if err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	get := func(url string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", url, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	sara, guest := get("/?user=Sara"), get("/")
	if !strings.Contains(sara, "Hello, Sara!") || !strings.Contains(guest, "Hello, guest!") {
		t.Fatalf("island per request:\n%s\n%s", sara, guest)
	}
	m := regexp.MustCompile(`data-brick-state="([^"]+)" data-brick-token="([0-9a-f]+)"`).FindStringSubmatch(sara)
	if m == nil {
		t.Fatal("no stateful component")
	}
	req := httptest.NewRequest(http.MethodPost, "/_brick/action", strings.NewReader(`{"handle":"`+m[1]+`","token":"`+m[2]+`","action":"flip"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Brick-Action", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), ">On</button>") {
		t.Fatalf("action: %d %s", rec.Code, rec.Body)
	}
	js := regexp.MustCompile(`src="(/_brick/c/Badge\.[^"]+)"`).FindStringSubmatch(sara)
	if js == nil || !strings.Contains(get(js[1]), `brick.mount("Badge"`) {
		t.Fatal("Badge module not served")
	}
	// RenderComponent: one component alone, for tests and previews.
	out, err := a.eng.RenderComponent(context.Background(), "Badge", nil, "beta")
	if err != nil || !strings.Contains(string(out), `<span data-brick=`) || !strings.Contains(string(out), ">beta</span>") {
		t.Fatalf("RenderComponent: %s %v", out, err)
	}
}
