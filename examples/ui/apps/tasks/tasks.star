# The Task board app's data and writes, over the tasks table. A task's
# position is its 0-based place within its status column; the seed numbers
# positions across the whole table, so every write first renumbers each
# column (normalize), which keeps the order and makes positions per column.

# STATUSES are the columns, in board order; an action may name one by its
# index (the keyboard "Move to…" menu) or its name (drag and drop).
STATUSES = ["todo", "doing", "done"]
LABELS = {"todo": "To do", "doing": "In progress", "done": "Done"}

# PRIORITIES and their Badge tones, most urgent first.
PRIORITIES = ["high", "medium", "low"]
TONES = {"high": "danger", "medium": "warning", "low": "neutral"}

# TODAY is the demo's clock for "overdue": fixed, so the board renders the
# same bytes on every run (the seeded due dates straddle it).
TODAY = "2026-09-27"

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

def short(iso):
    return MONTHS[int(iso[5:7], 10) - 1] + " " + str(int(iso[8:10], 10))

def board(input):
    """Every task, grouped by column, with display fields; the component filters."""
    rows = db.query("select id, title, status, priority, assignee, due, position from tasks order by position, id")
    tasks = []
    for r in rows:
        t = dict(r)
        t["priorityTone"] = TONES.get(r["priority"], "neutral")
        t["statusLabel"] = LABELS.get(r["status"], r["status"])
        t["dueLabel"] = short(r["due"]) if len(r["due"]) == 10 else r["due"]
        t["overdue"] = r["status"] != "done" and r["due"] < TODAY
        tasks.append(t)
    columns = [{"status": s, "index": i, "label": LABELS[s], "tasks": [t for t in tasks if t["status"] == s]} for i, s in enumerate(STATUSES)]
    people = sorted({r["assignee"]: True for r in rows}.keys())
    return {"columns": columns, "tasks": tasks, "people": people, "priorities": PRIORITIES, "today": TODAY}

def normalize():
    for s in STATUSES:
        for i, r in enumerate(db.query("select id from tasks where status = ? order by position, id", s)):
            db.exec("update tasks set position = ? where id = ?", i, r["id"])

def status_of(s):
    if type(s) == "int":
        return STATUSES[s] if s >= 0 and s < len(STATUSES) else ""
    return s if s in STATUSES else ""

def move(id, status, position):
    """Puts task id at position (clamped) in column status, closing the gap it
    leaves and opening one where it lands. Returns False for an unknown task
    or column."""
    status = status_of(status)
    cur = db.query("select status, position from tasks where id = ?", id)
    if not status or not cur:
        return False
    normalize()
    cur = db.query("select status, position from tasks where id = ?", id)[0]
    db.exec("update tasks set position = position - 1 where status = ? and position > ?", cur["status"], cur["position"])
    n = db.query("select count(*) as n from tasks where status = ? and id != ?", status, id)[0]["n"]
    pos = min(max(0, position), n)
    db.exec("update tasks set position = position + 1 where status = ? and position >= ? and id != ?", status, pos, id)
    db.exec("update tasks set status = ?, position = ? where id = ?", status, pos, id)
    return True

def validate(fields):
    """The task fields from a form, cleaned, and the errors by field name."""
    t = {
        "title": fields.get("title", "").strip(),
        "priority": fields.get("priority", ""),
        "assignee": fields.get("assignee", "").strip(),
        "due": fields.get("due", "").strip(),
        "status": fields.get("status", "todo"),
    }
    errors = {}
    if len(t["title"]) < 3:
        errors["title"] = "Give the task a title of at least 3 characters."
    elif len(t["title"]) > 80:
        errors["title"] = "Keep the title under 80 characters."
    if t["priority"] not in PRIORITIES:
        errors["priority"] = "Choose a priority."
    if not t["assignee"]:
        errors["assignee"] = "Choose who does it."
    if not t["due"] or db.query("select date(?) is ? as ok", t["due"], t["due"])[0]["ok"] != 1:
        errors["due"] = "Pick a due date."
    if t["status"] not in STATUSES:
        errors["status"] = "Choose a column."
    return t, errors

def add(t):
    """Inserts a validated task at the end of its column; returns its id."""
    normalize()
    n = db.query("select count(*) as n from tasks where status = ?", t["status"])[0]["n"]
    db.exec("insert into tasks (title, status, priority, assignee, due, position) values (?, ?, ?, ?, ?, ?)", t["title"], t["status"], t["priority"], t["assignee"], t["due"], n)
    return db.query("select max(id) as id from tasks")[0]["id"]

def update(id, t):
    """Saves a validated task; a changed column moves it to that column's end."""
    cur = db.query("select status from tasks where id = ?", id)
    if not cur:
        return False
    db.exec("update tasks set title = ?, priority = ?, assignee = ?, due = ? where id = ?", t["title"], t["priority"], t["assignee"], t["due"], id)
    if cur[0]["status"] != t["status"]:
        move(id, t["status"], 1000000)
    return True

def remove(id):
    db.exec("delete from tasks where id = ?", id)
    normalize()
