package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/brick/examples/ui/demodb"
)

var (
	chatOpenRe = regexp.MustCompile(`brick:click="chat\.open_channel\((\d+)\)"`)
	chatNameRe = regexp.MustCompile(`<span class="chat__item-name">([^<]+)</span>`)
)

// chatIDs maps each sidebar conversation's name to the id its button opens,
// read from the page: buttons and names appear in the same order.
func chatIDs(t *testing.T, page string) map[string]string {
	t.Helper()
	ids, names := chatOpenRe.FindAllStringSubmatch(page, -1), chatNameRe.FindAllStringSubmatch(page, -1)
	if len(ids) == 0 || len(ids) != len(names) {
		t.Fatalf("sidebar: %d buttons, %d names", len(ids), len(names))
	}
	out := map[string]string{}
	for i := range ids {
		out[names[i][1]] = ids[i][1]
	}
	return out
}

// The page renders the sidebar with seeded unread counts, #general's
// conversation grouped by day and sender, the composer and the drawer.
func TestChatPage(t *testing.T) {
	_, h := showcase(t)
	code, body := get(t, h, "/apps/chat/")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	landCallsParse(t, body)
	mustContain(t, "chat", body,
		`brick:poll="5s"`, `data-brick-type="ChatApp"`, `aria-current="true"><svg`, `general</h1>`,
		`aria-label="2 unread"`, `aria-label="3 unread"`, `aria-label="1 unread"`,
		`Friday, Sep 25`, `Morning all! The v2 launch plan is pinned in #launch-v2.`, `Thanks Noah — I&#39;ll share the agenda by Friday.`,
		`brick:submit="chat.send"`, `name="body"`, `placeholder="Message #general"`, `data-emoji="🚀"`, `brick:click="chat.share(0)"`,
		`Members · 6`, `Omar Haddad`)
	ids := chatIDs(t, body)
	for _, n := range []string{"general", "design", "engineering", "launch-v2", "Maya Chen", "Liam Kim", "Brickbot", "Noah Silva"} {
		if ids[n] == "" {
			t.Errorf("sidebar is missing %s", n)
		}
	}
}

// Sending inserts a row that appears in the conversation, stamped from the
// channel's clock rather than the wall time; an empty message is refused
// with an error and inserts nothing.
func TestChatSend(t *testing.T) {
	_, h := showcase(t)
	s := landOpen(t, h, "/apps/chat/")
	out := s.act("chat.send", map[string]string{"body": "   "})
	mustContain(t, "empty", out, "Write a message first.", `role="alert"`)
	out = s.act("chat.send", map[string]string{"body": "Ship it on Thursday?"})
	first, second := chatNextTimes()
	mustContain(t, "send", out, "Ship it on Thursday?", ">"+first+"<", "Delivered", "Today")
	if strings.Contains(out, "Write a message first.") {
		t.Error("the error outlived a good send")
	}
	out = s.act("chat.send", map[string]string{"body": "Second"})
	mustContain(t, "second send", out, ">"+second+"<")
	if n := strings.Count(out, "Delivered"); n != 1 {
		t.Errorf("%d messages say Delivered, want only the newest of mine", n)
	}
	out = s.act("chat.share", 1)
	mustContain(t, "share", out, "launch-checklist.xlsx", "Shared a file")
}

// Opening a channel shows it and clears its unread count, which stays
// cleared after leaving; other channels keep theirs.
func TestChatChannels(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/chat/")
	ids := chatIDs(t, page)
	s := landOpen(t, h, "/apps/chat/")
	out := s.act("chat.open_channel", ids["design"])
	mustContain(t, "design", out, "design</h1>", "Option B tested best", "pricing-v3.fig", `placeholder="Message #design"`, `aria-label="3 unread"`)
	if strings.Contains(out, `aria-label="2 unread"`) {
		t.Error("design still shows 2 unread while open")
	}
	out = s.act("chat.open_channel", ids["general"])
	if strings.Contains(out, `aria-label="2 unread"`) {
		t.Error("design's unread count came back after leaving it")
	}
	mustContain(t, "general again", out, `aria-label="3 unread"`, `aria-label="1 unread"`)
	out = s.act("chat.open_channel", ids["Maya Chen"])
	mustContain(t, "dm", out, "Maya Chen</h1>", "Sure! How about 10:30?", "Product design lead")
	out = s.act("chat.open_channel", 999)
	mustContain(t, "unknown id keeps the channel", out, "Maya Chen</h1>")
	out = s.act("chat.search", "des")
	mustContain(t, "search", out, `>design</span>`)
	if strings.Contains(out, `>engineering</span>`) {
		t.Error("search did not filter the sidebar")
	}
}

// Brickbot's reply is queued by the send and delivered by the next render,
// which is what the page's 5-second poll ($refresh) triggers.
func TestChatBotReplyArrivesOnPoll(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/chat/")
	ids := chatIDs(t, page)
	s := landOpen(t, h, "/apps/chat/")
	s.act("chat.open_channel", ids["Brickbot"])
	out := s.act("chat.send", map[string]string{"body": "Hello bot"})
	mustContain(t, "after send", out, "Hello bot", "Brickbot is typing…")
	if strings.Contains(out, "Nice one.") {
		t.Error("the reply arrived before the poll")
	}
	out = s.act("$refresh")
	// The reply cycles by the channel's message count: the seeded greeting
	// plus "Hello bot" is 2, so it is BOT_REPLIES[2].
	mustContain(t, "after poll", out, "Nice one. Unread counts come from last_read", "Read")
	if strings.Contains(out, "is typing") {
		t.Error("still typing after the reply was delivered")
	}
	out = s.act("chat.open_channel", ids["general"])
	if strings.Contains(out, `>Brickbot</span><span class="bk-badge`) {
		t.Error("the delivered reply was counted unread although the DM was open")
	}
	mustContain(t, "Maya still unread", out, `>Maya Chen</span><span class="bk-badge`)
}

// chatNextTimes is the stamp of the first and second message sent to
// #general today. apps/chat/chat.star continues the channel's clock a minute after
// its last message today, else starts at 09:00; the seed's last day is
// 2026-09-27, whose #general ends at 08:48, so the stamps depend on
// whether the test runs on that day.
func chatNextTimes() (string, string) {
	if time.Now().UTC().Format(time.DateOnly) == demodb.ChatLastDay() {
		return "08:49", "08:50"
	}
	return "09:00", "09:01"
}
