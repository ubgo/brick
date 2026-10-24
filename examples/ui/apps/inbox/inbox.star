# The Inbox demo app: folders, the message list and the reading pane over
# the messages table in demodb/db_inbox.go, plus the actions InboxApp calls
# (brick:click="inbox.archive(3)").
#
# folder is one of FOLDERS; "starred" is a view over every folder but trash,
# and "sent" holds what compose writes (demodb/db_inbox.go documents inbox, archive and
# trash for the seed; the column is free text, so sent needs no migration).
#
# Every action returns the new state and sets flash (the toast text) and
# compose_open. Only the action that means it sets either; every other one
# clears both, so a toast or the compose dialog never comes back on an
# unrelated re-render.

FOLDERS = ["inbox", "starred", "sent", "archive", "trash"]

LABELS = {"inbox": "Inbox", "starred": "Starred", "sent": "Sent", "archive": "Archive", "trash": "Trash"}

ICONS = {"inbox": "inbox", "starred": "star", "sent": "send", "archive": "archive", "trash": "trash"}

# ME is the signed-in person: the sender of everything compose writes.
ME = "Ada Lovelace"

# SNIPPET is how many characters of the body a list row previews.
SNIPPET = 90

Q_FOLDER = "select id, sender, email, subject, body, folder, unread, starred, sent from messages where folder = ? and (? = '' or lower(sender || ' ' || subject || ' ' || body) like ?) order by sent desc, id desc"
Q_STARRED = "select id, sender, email, subject, body, folder, unread, starred, sent from messages where starred = 1 and folder != 'trash' and (? = '' or lower(sender || ' ' || subject || ' ' || body) like ?) order by sent desc, id desc"

def _row(m):
    first = [l for l in m["body"].split("\n") if l.strip() and not l.strip().endswith(",")]
    text = " ".join(first) if first else m["body"]
    return dict(m, snippet = text[:SNIPPET] + ("…" if len(text) > SNIPPET else ""), unread = m["unread"] == 1, starred = m["starred"] == 1)

def folders(state):
    rows = db.query("select folder, count(*) as n, sum(unread) as unread from messages group by folder")
    by = {r["folder"]: r for r in rows}
    starred = db.query("select count(*) as n, sum(unread) as unread from messages where starred = 1 and folder != 'trash'")[0]
    out = []
    for i, f in enumerate(FOLDERS):
        r = starred if f == "starred" else by.get(f, {"n": 0, "unread": 0})
        out.append({"i": i, "id": f, "label": LABELS[f], "icon": ICONS[f], "n": r["n"] or 0, "unread": r["unread"] or 0, "on": f == state["folder"]})
    return out

def messages(state):
    q = state["q"].strip().lower()
    like = "%" + q + "%"
    if state["folder"] == "starred":
        rows = db.query(Q_STARRED, q, like)
    else:
        rows = db.query(Q_FOLDER, state["folder"], q, like)
    return [_row(m) for m in rows]

def message(state):
    if not state["selected"]:
        return None
    rows = db.query("select id, sender, email, subject, body, folder, unread, starred, sent from messages where id = ?", state["selected"])
    return _row(rows[0]) if rows else None

def _quiet(state, **kw):
    return dict(state, flash = "", compose_open = False, **kw)

def _flash(state, text, **kw):
    return dict(state, flash = text, flashes = state["flashes"] + 1, compose_open = False, **kw)

# ---- actions ------------------------------------------------------------

def open_folder(state, props, i):
    return _quiet(state, folder = FOLDERS[i] if i >= 0 and i < len(FOLDERS) else "inbox", selected = 0)

def search(state, props, q):
    return _quiet(state, q = q.strip(), selected = 0)

def clear_search(state, props):
    return _quiet(state, q = "", selected = 0)

def select(state, props, id):
    db.exec("update messages set unread = 0 where id = ?", id)
    return _quiet(state, selected = id)

def back(state, props):
    return _quiet(state, selected = 0)

def star(state, props, id):
    db.exec("update messages set starred = 1 - starred where id = ?", id)
    return _quiet(state)

def mark_unread(state, props, id):
    db.exec("update messages set unread = 1 where id = ?", id)
    return _flash(state, "Marked as unread", selected = 0)

def archive(state, props, id):
    rows = db.query("select folder from messages where id = ?", id)
    if not rows:
        return _quiet(state, selected = 0)
    if rows[0]["folder"] == "archive":
        db.exec("update messages set folder = 'inbox' where id = ?", id)
        return _flash(state, "Moved to Inbox", selected = 0)
    db.exec("update messages set folder = 'archive' where id = ?", id)
    return _flash(state, "Conversation archived", selected = 0)

def delete(state, props, id):
    rows = db.query("select folder from messages where id = ?", id)
    if rows and rows[0]["folder"] == "trash":
        db.exec("delete from messages where id = ?", id)
        return _flash(state, "Deleted forever", selected = 0)
    db.exec("update messages set folder = 'trash' where id = ?", id)
    return _flash(state, "Moved to Trash", selected = 0)

def reply(state, props, id):
    rows = db.query("select email, subject from messages where id = ?", id)
    if not rows:
        return _quiet(state)
    subject = rows[0]["subject"]
    return dict(state, flash = "", compose_open = True, draft = {"to": rows[0]["email"], "subject": subject if subject.startswith("Re: ") else "Re: " + subject, "body": ""})

def new_message(state, props):
    return dict(state, flash = "", compose_open = True, draft = {"to": "", "subject": "", "body": ""})

def compose(state, props, fields):
    d = {k: fields.get(k, "").strip() for k in ["to", "subject", "body"]}
    errors = {}
    at = d["to"].find("@")
    if at < 1 or "." not in d["to"][at:] or " " in d["to"]:
        errors["to"] = "Enter one e-mail address, like grace@example.com."
    if not d["subject"]:
        errors["subject"] = "Add a subject."
    if len(d["body"]) < 2:
        errors["body"] = "Write a message."
    if errors:
        return [dict(state, draft = d, flash = "", compose_open = True), errors]
    db.exec("insert into messages (sender, email, subject, body, folder, unread, starred, sent) values (?, ?, ?, ?, 'sent', 0, 0, ?)", ME, d["to"], d["subject"], d["body"], today())
    return _flash(state, "Message sent to " + d["to"], draft = {"to": "", "subject": "", "body": ""})
