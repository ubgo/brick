package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// tasksURL is the Task board app.
const tasksURL = "/apps/tasks/"

var (
	tasksColRe  = regexp.MustCompile(`(?s)data-action="move" data-group="board" data-list="(\w+)".*?</ul>`)
	tasksCardRe = regexp.MustCompile(`<li class="tb-card" data-id="(\d+)"`)
)

// tasksColumns reads the board's columns: status → card ids, top to bottom.
func tasksColumns(html string) map[string][]string {
	out := map[string][]string{}
	for _, m := range tasksColRe.FindAllStringSubmatch(html, -1) {
		ids := []string{}
		for _, c := range tasksCardRe.FindAllStringSubmatch(m[0], -1) {
			ids = append(ids, c[1])
		}
		out[m[1]] = ids
	}
	return out
}

// tasksWant fails unless column status holds exactly ids, in order.
func tasksWant(t *testing.T, what, html, status string, ids ...string) {
	t.Helper()
	if got := tasksColumns(html)[status]; !slices.Equal(got, ids) {
		t.Errorf("%s: %s column is %v, want %v", what, status, got, ids)
	}
}

// The board renders the seeded tasks: four per column in seed order, the
// overdue ones (due before the demo's fixed today, Sep 27, and not done)
// marked, each column a grouped Sortable calling move, and each card's
// menu calling the same action.
func TestTasksPage(t *testing.T) {
	_, h := showcase(t)
	_, page := dashOpen(t, h, tasksURL)
	tasksWant(t, "seed", page, "todo", "1", "4", "7", "10")
	tasksWant(t, "seed", page, "doing", "2", "5", "8", "11")
	tasksWant(t, "seed", page, "done", "3", "6", "9", "12")
	mustContain(t, tasksURL, page,
		`href="/apps/tasks/" aria-current="page"`, `12 of 12 tasks`,
		`tb-due tb-due--overdue" title="Overdue"`, `Sep 12<span class="bk-sr-only"> (overdue)</span>`,
		`brick:click="move(1, 1, 999)"`, `brick:click="move(1, 2, 999)"`, `brick:click="edit(1)"`, `brick:click="remove(1)"`,
		`role="tablist" aria-label="View"`, `>Table</button>`,
		`brick:change="assignee"`, `brick:change="priority"`,
		`class="bk-sortable bk-sortable--vertical bk-sortable--grouped" data-action="move" data-group="board" data-list="doing"`,
		`aria-label="In progress"`, `<li class="tb-empty">Drop a task here</li>`,
	)
	// A done task is never overdue, however old: T-3 was due Sep 18.
	t3 := strings.Split(strings.Split(page, `<li class="tb-card" data-id="3"`)[1], `class="tb-card__who"`)[0]
	if !strings.Contains(t3, "Sep 18") || strings.Contains(t3, "tb-due--overdue") {
		t.Errorf("T-3 (done, due Sep 18) must show its date and not be overdue:\n%s", t3)
	}
}

// Every write is an action that changes the table; a fresh page load (which
// reads the table again) shows the result.
func TestTasksActions(t *testing.T) {
	_, h := showcase(t)
	s, _ := dashOpen(t, h, tasksURL)

	// Drag and drop: T-2 from In progress to the top of To do.
	out := s.act("move", 2, "todo", 0)
	tasksWant(t, "move across", out, "todo", "2", "1", "4", "7", "10")
	tasksWant(t, "move across", out, "doing", "5", "8", "11")
	// Within a column: T-10 up to second place.
	out = s.act("move", 10, "todo", 1)
	tasksWant(t, "move within", out, "todo", "2", "10", "1", "4", "7")
	// The keyboard menu: column by index, 999 = the end.
	out = s.act("move", 1, 2, 999)
	tasksWant(t, "menu move", out, "done", "3", "6", "9", "12", "1")
	// An unknown column changes nothing.
	out = s.act("move", 4, "later", 0)
	tasksWant(t, "bad column", out, "todo", "2", "10", "4", "7")

	// Add: an empty form comes back with every error and the dialog open.
	s.act("startAdd")
	out = s.act("save", map[string]string{"title": "x", "status": "todo"})
	mustContain(t, "invalid add", out, "Give the task a title of at least 3 characters.", "Choose a priority.", "Choose who does it.", "Pick a due date.", `value="x"`)
	if !regexp.MustCompile(`<dialog[^>]*id="tb-task"[^>]*\sopen`).MatchString(out) {
		t.Error("the dialog closed on a failed save")
	}
	out = s.act("save", map[string]string{"title": "Write release notes", "status": "doing", "priority": "high", "assignee": "Ava", "due": "2026-10-20"})
	mustContain(t, "valid add", out, "13 of 13 tasks", "Write release notes")
	tasksWant(t, "valid add", out, "doing", "5", "8", "11", "13")

	// Edit T-5: the dialog opens filled in; saving to another column moves it.
	out = s.act("edit", 5)
	mustContain(t, "edit", out, "Edit task", `value="Review the Q3 roadmap"`)
	out = s.act("save", map[string]string{"title": "Review the Q4 roadmap", "status": "done", "priority": "low", "assignee": "Zara", "due": "2026-10-01"})
	mustContain(t, "edited", out, "Review the Q4 roadmap")
	tasksWant(t, "edited", out, "done", "3", "6", "9", "12", "1", "5")

	// Delete, and the filters.
	out = s.act("remove", 3)
	mustContain(t, "remove", out, "12 of 12 tasks")
	tasksWant(t, "remove", out, "done", "6", "9", "12", "1", "5")
	out = s.act("priority", "high")
	mustContain(t, "filter", out, `<option value="high" selected>High</option>`)
	// High: T-1 and T-9 from the seed and the new T-13; T-5 was edited to low.
	var high []string
	for _, m := range tasksCardRe.FindAllStringSubmatch(out, -1) {
		high = append(high, m[1])
	}
	if !slices.Equal(high, []string{"13", "9", "1"}) {
		t.Errorf("priority high shows %v, want T-13 (in progress), T-9 and T-1 (done)", high)
	}
	out = s.act("clearFilters")
	mustContain(t, "clear", out, "12 of 12 tasks")

	// A fresh load reads the table: the writes are in the database.
	_, page := get(t, h, tasksURL)
	tasksWant(t, "reload", page, "todo", "2", "10", "4", "7")
	tasksWant(t, "reload", page, "doing", "8", "11", "13")
	tasksWant(t, "reload", page, "done", "6", "9", "12", "1", "5")
}

// Drag and drop on the real page, in jsdom with the page's runtime and the
// kit Sortable module: dragging T-2 from In progress over the second card
// of To do and dropping calls move with [2, "todo", 1]; the keyboard hop
// sends the same shape (testdata/tasks_drag.mjs).
func TestTasksDragInBrowser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	jsdom, _ := filepath.Abs("../../runtime/test/node_modules/jsdom")
	if _, err := os.Stat(jsdom); err != nil {
		t.Skip("jsdom is not installed: run `task js:deps`")
	}
	_, h := showcase(t)
	_, page := get(t, h, tasksURL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime", "Sortable", "TaskBoard"} {
		src := regexp.MustCompile(`/_brick/c/` + name + `\.[0-9a-f]+\.js`).FindString(page)
		if src == "" {
			t.Fatalf("no %s module in the page", name)
		}
		code, js := get(t, h, src)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", src, code)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(js), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script, _ := filepath.Abs("testdata/tasks_drag.mjs")
	cmd := exec.CommandContext(t.Context(), node, script, dir)
	cmd.Env = append(os.Environ(), "JSDOM_PATH="+jsdom)
	res, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(res), "tasks_drag ok") {
		t.Fatalf("tasks_drag: %v\n%s", err, res)
	}
}
