# The Chat demo app (content/apps/chat.html, apps/chat/ChatApp.html):
# the channel list, the open conversation and every ChatApp action, over the
# channels, channel_members and chat_messages tables in demodb/db_chat.go.
#
# State is {channel, q}: the open channel's slug and the sidebar search.
# Actions get (state, props, args…), write with db.exec and return the new
# state, or [state, errors] when a message cannot be sent.
#
# Two writes happen at render time, in deliver(), because they are what
# viewing does in a real chat: the open channel is marked read, and a
# Brickbot reply the last send queued (pending = 1) is delivered. The page
# polls every 5 seconds, so a reply appears on the refresh after the send —
# the demo's stand-in for a message from another person.

ME = "Ada Lovelace"
BOT = "Brickbot"
BOT_SLUG = "brickbot"

# MAX_BODY is the longest message accepted, in characters.
MAX_BODY = 2000

# FILES are what the attach menu can share (a demo has no upload).
FILES = ["roadmap-q4.pdf", "launch-checklist.xlsx", "hero-mockup.png"]

# EMOJI are the composer's quick picks; the client module inserts them.
EMOJI = ["👍", "🎉", "❤️", "😂", "🙏", "👀", "🚀", "✅"]

# BOT_REPLIES cycle by message count, so a run is reproducible.
BOT_REPLIES = [
    "Got it! This reply was written by a Starlark action and delivered on the next poll.",
    "Beep boop. Every message here is a row in SQLite — try switching channels and coming back.",
    "Nice one. Unread counts come from last_read, so leave this DM and I'll make the badge light up next time.",
    "I'm only a demo, but the conversation you see is rendered on the server, fresh on every refresh.",
]

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]

Q_CHANNELS = "select c.id, c.slug, c.name, c.kind, c.topic, c.presence, (select count(*) from chat_messages m where m.channel_id = c.id and m.id > c.last_read and m.sender != ? and m.pending = 0) as unread from channels c where (? = '' or lower(c.name) like ?) order by c.id"
Q_CHANNEL = "select id, slug, name, kind, topic, presence from channels where slug = ?"
Q_MEMBERS = "select name, role from channel_members where channel_id = ? order by case role when 'owner' then 0 when 'bot' then 1 else 2 end, name"
Q_MESSAGES = "select id, sender, body, day, time, attachment from chat_messages where channel_id = ? and pending = 0 order by id"
Q_FILES = "select sender, attachment, day from chat_messages where channel_id = ? and attachment != '' and pending = 0 order by id desc"
Q_PENDING = "select count(*) as n from chat_messages where channel_id = ? and pending = 1"
Q_LAST_TODAY = "select time from chat_messages where channel_id = ? and day = ? order by id desc limit 1"
Q_COUNT = "select count(*) as n from chat_messages where channel_id = ?"

# ---- dates ----------------------------------------------------------------

def _days(iso):
    # Days since 1970-01-01 for an ISO date (Howard Hinnant's days_from_civil).
    y, m, d = int(iso[0:4]), int(iso[5:7]), int(iso[8:10])
    if m <= 2:
        y -= 1
    era = y // 400
    yoe = y - era * 400
    mp = (m + 9) % 12
    doy = (153 * mp + 2) // 5 + d - 1
    doe = yoe * 365 + yoe // 4 - yoe // 100 + doy
    return era * 146097 + doe - 719468

def day_label(iso, now):
    # "Today", "Yesterday", or "Friday, Sep 25".
    n = _days(now) - _days(iso)
    if n == 0:
        return "Today"
    if n == 1:
        return "Yesterday"
    return "%s, %s %d" % (WEEKDAYS[(_days(iso) + 4) % 7], MONTHS[int(iso[5:7]) - 1], int(iso[8:10]))

def _next_time(cid, now):
    # One minute after the channel's last message today, or 09:00: a clock
    # that never reads the wall time, so tests see the same stamps.
    rows = db.query(Q_LAST_TODAY, cid, now)
    if not rows:
        return "09:00"
    t = int(rows[0]["time"][0:2]) * 60 + int(rows[0]["time"][3:5]) + 1
    t = min(t, 23 * 60 + 59)
    return _pad(t // 60) + ":" + _pad(t % 60)

def _pad(n):
    # Starlark's % has no width flags, so %02d is spelled out.
    return ("0" + str(n))[-2:]

# ---- views ----------------------------------------------------------------

def channel(state):
    rows = db.query(Q_CHANNEL, state["channel"])
    if not rows:
        rows = db.query(Q_CHANNEL, "general")
    c = rows[0]
    members = db.query(Q_MEMBERS, c["id"])
    return dict(c, members = members, others = [m for m in members if m["name"] != ME], files = db.query(Q_FILES, c["id"]))

def sidebar(state):
    q = state["q"].strip().lower()
    rows = db.query(Q_CHANNELS, ME, q, "%" + q + "%")
    out = {"channels": [], "dms": [], "total": 0}
    for r in rows:
        r = dict(r, on = r["slug"] == state["channel"])
        if r["on"]:
            r["unread"] = 0
        out["total"] += r["unread"]
        out["channels" if r["kind"] == "channel" else "dms"].append(r)
    return out

def conversation(state):
    # The open channel's messages as days, each a list of runs of one
    # sender, so the page shows the name and avatar once per run.
    c = channel(state)
    rows = db.query(Q_MESSAGES, c["id"])
    now = today()
    last_mine = 0
    last_other = 0
    for r in rows:
        if r["sender"] == ME:
            last_mine = r["id"]
        else:
            last_other = r["id"]
    days = []
    for r in rows:
        if not days or days[-1]["day"] != r["day"]:
            days.append({"day": r["day"], "label": day_label(r["day"], now), "runs": []})
        runs = days[-1]["runs"]
        mine = r["sender"] == ME
        status = ""
        if r["id"] == last_mine:
            status = "Read" if last_other > last_mine else "Delivered"
        item = dict(r, status = status)
        if runs and runs[-1]["sender"] == r["sender"]:
            runs[-1]["items"].append(item)
        else:
            runs.append({"sender": r["sender"], "mine": mine, "bot": r["sender"] == BOT, "items": [item]})
    return days

def typing(state):
    c = channel(state)
    return db.query(Q_PENDING, c["id"])[0]["n"] > 0

def deliver(state):
    # Render-time bookkeeping (see the header): deliver queued bot replies
    # and mark the open channel read. Returns "" so the template prints
    # nothing.
    c = channel(state)
    db.exec("update chat_messages set pending = 0 where channel_id = ? and pending = 1", c["id"])
    db.exec("update channels set last_read = coalesce((select max(id) from chat_messages where channel_id = ?), 0) where id = ?", c["id"], c["id"])
    return ""

# ---- actions ----------------------------------------------------------------

def open_channel(state, props, id):
    rows = db.query("select slug from channels where id = ?", id)
    if not rows:
        return state
    return dict(state, channel = rows[0]["slug"])

def search(state, props, q):
    return dict(state, q = q.strip())

def clear_search(state, props):
    return dict(state, q = "")

def send(state, props, fields):
    body = fields.get("body", "").strip()
    if not body:
        return [state, {"body": "Write a message first."}]
    if len(body) > MAX_BODY:
        return [state, {"body": "Messages are at most %d characters." % MAX_BODY}]
    c = channel(state)
    now = today()
    db.exec("insert into chat_messages (channel_id, sender, body, day, time, attachment, pending) values (?, ?, ?, ?, ?, '', 0)", c["id"], ME, body, now, _next_time(c["id"], now))
    if c["slug"] == BOT_SLUG:
        n = db.query(Q_COUNT, c["id"])[0]["n"]
        db.exec("insert into chat_messages (channel_id, sender, body, day, time, attachment, pending) values (?, ?, ?, ?, ?, '', 1)", c["id"], BOT, BOT_REPLIES[n % len(BOT_REPLIES)], now, _next_time(c["id"], now))
    return state

def share(state, props, i):
    if i < 0 or i >= len(FILES):
        return state
    c = channel(state)
    now = today()
    db.exec("insert into chat_messages (channel_id, sender, body, day, time, attachment, pending) values (?, ?, 'Shared a file', ?, ?, ?, 0)", c["id"], ME, now, _next_time(c["id"], now), FILES[i])
    return state

# ---- constants for the template (a module's values are reached through functions) ----

def files():
    return FILES

def emoji():
    return EMOJI
