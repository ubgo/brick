package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// inboxMust runs one action on the Inbox page's stateful component (with
// the Store tests' protocol helper) and fails unless it succeeded.
func inboxMust(t *testing.T, h http.Handler, page, action string, args ...any) string {
	t.Helper()
	code, out := storeAction(t, h, page, action, args...)
	if code != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", action, code, out)
	}
	return out
}

// inboxCount reads a folder's "N conversations" heading.
func inboxCount(html string) string {
	m := regexp.MustCompile(`<small>(\d+) conversations?</small>`).FindStringSubmatch(html)
	if m == nil {
		return "?"
	}
	return m[1]
}

// The Inbox page renders the folders, the inbox list from the messages
// table and the empty reading pane.
func TestInboxPage(t *testing.T) {
	_, h := showcase(t)
	code, page := get(t, h, "/apps/inbox/")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, page)
	}
	mustContain(t, "inbox", page,
		`href="/apps/inbox/" aria-current="page"`, `aria-label="Folders"`,
		`brick:click="inbox.open_folder(0)"`, `aria-current="true"><svg`, `<span class="inbox__folder-label">Trash</span>`,
		`<small>14 conversations</small>`, `inbox-row--unread`, `brick:click="inbox.select(1)"`, `brick:click="inbox.star(1)"`,
		`No message selected`, `<h1 class="bk-sr-only">Inbox</h1>`, `id="inbox-compose"`, `brick:submit="inbox.compose"`, `brick:change="inbox.search"`,
	)
}

// Opening marks read, star toggles, archive moves the message, delete goes
// to Trash and then for good, and search filters: every step is a write to
// the messages table read back through the next render.
func TestInboxActions(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/inbox/")

	out := inboxMust(t, h, page, "inbox.select", 1)
	mustContain(t, "open", out, `<h1 class="inbox__subject">Quarterly numbers are in</h1>`, `brick:click="inbox.archive(1)"`)
	if regexp.MustCompile(`class="inbox-row inbox-row--unread" aria-current="true"`).MatchString(out) {
		t.Error("the opened message is still unread")
	}
	out = inboxMust(t, h, page, "inbox.star", 2)
	mustContain(t, "star", out, `inbox-star--on`)
	out = inboxMust(t, h, page, "inbox.open_folder", 1)
	if got := inboxCount(out); got != "5" {
		t.Errorf("starred: %s conversations, want the 4 seeded plus #2", got)
	}

	out = inboxMust(t, h, page, "inbox.open_folder", 0)
	before := inboxCount(out)
	out = inboxMust(t, h, page, "inbox.archive", 1)
	mustContain(t, "archived", out, `Conversation archived`)
	if got := inboxCount(out); got == before {
		t.Errorf("archive left the inbox at %s", got)
	}
	out = inboxMust(t, h, page, "inbox.open_folder", 3)
	mustContain(t, "archive folder", out, `Quarterly numbers are in`, `brick:click="inbox.select(1)"`)

	out = inboxMust(t, h, page, "inbox.delete", 1)
	mustContain(t, "trashed", out, `Moved to Trash`)
	out = inboxMust(t, h, page, "inbox.open_folder", 4)
	if got := inboxCount(out); got != "1" {
		t.Errorf("trash holds %s", got)
	}
	out = inboxMust(t, h, page, "inbox.delete", 1)
	mustContain(t, "deleted", out, `Deleted forever`, `Trash is empty`)

	out = inboxMust(t, h, page, "inbox.open_folder", 0)
	mustContain(t, "back to the inbox", out, `<h2>Inbox</h2>`)
	if got := inboxCount(out); got == "0" || got == before {
		t.Errorf("inbox after archiving one: %s conversations (was %s)", got, before)
	}
	out = inboxMust(t, h, page, "inbox.search", "lunch")
	if got := inboxCount(out); got == "0" || got == before {
		t.Errorf("search for lunch: %s conversations", got)
	}
	mustContain(t, "search", out, `Lunch next week?`)
	if strings.Contains(out, "Design review notes") {
		t.Error("search for lunch still lists the design review")
	}
}

// Compose validates, then writes the message to Sent; the dialog stays
// open only while it has errors to show.
func TestInboxCompose(t *testing.T) {
	_, h := showcase(t)
	_, page := get(t, h, "/apps/inbox/")
	out := inboxMust(t, h, page, "inbox.compose", map[string]string{"to": "nope", "subject": "", "body": ""})
	mustContain(t, "invalid", out, `Enter one e-mail address, like grace@example.com.`, `Add a subject.`, `Write a message.`, `value="nope"`)
	if !regexp.MustCompile(`<dialog[^>]*id="inbox-compose"[^>]*\sopen|<dialog[^>]*\sopen[^>]*id="inbox-compose"`).MatchString(out) {
		t.Error("the compose dialog closed over its errors")
	}
	out = inboxMust(t, h, page, "inbox.compose", map[string]string{"to": "grace@example.com", "subject": "Lunch?", "body": "Friday at noon?"})
	mustContain(t, "sent", out, `Message sent to grace@example.com`)
	out = inboxMust(t, h, page, "inbox.open_folder", 2)
	mustContain(t, "sent folder", out, `To: grace@example.com`, `Lunch?`)
	out = inboxMust(t, h, page, "inbox.reply", 3)
	mustContain(t, "reply", out, `value="Re: Design review notes"`)
}
