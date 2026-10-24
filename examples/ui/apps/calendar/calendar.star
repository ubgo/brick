# The Calendar demo app (content/apps/calendar.html,
# apps/calendar/CalendarApp.html): the month, week and agenda views and
# every CalendarApp action, over the events table in demodb/db_calendar.go.
#
# State is {view, cursor, form_open, draft, flash, flashes}: the view, the
# date the view is built around, the event dialog and its draft, and the
# last toast. Dates are days since 1970-01-01 inside this file and ISO
# strings everywhere else; the page never reads the clock (TODAY), so a
# render gives the same bytes on every run.
#
# Every action clears flash except the one that means it, so a toast never
# comes back on an unrelated re-render; flashes counts toasts so each has a
# fresh id (the Toaster shows an id once).

TODAY = "2026-09-27"

VIEWS = ["month", "week", "agenda"]

# TONES is the closed set of event colours, in picker order; the page maps
# each to a --bk-* token.
TONES = ["primary", "info", "success", "warning", "danger", "neutral"]
TONE_LABELS = {"primary": "Indigo", "info": "Blue", "success": "Green", "warning": "Amber", "danger": "Red", "neutral": "Grey"}

# MONTH_CHIPS is how many events a month cell lists before "+N more".
MONTH_CHIPS = 3

# The week view's visible hours: HOUR_START:00 to HOUR_END:00. Events
# outside are clamped to the edge so they still show.
HOUR_START = 7
HOUR_END = 21

MAX_TITLE = 120

MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"]
WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]

Q_RANGE = "select id, title, start, end, all_day, tone, location, notes, attendees from events where substr(start, 1, 10) <= ? and substr(end, 1, 10) >= ? order by all_day desc, start, id"
Q_ONE = "select id, title, start, end, all_day, tone, location, notes, attendees from events where id = ?"
Q_UPCOMING = "select id, title, start, end, all_day, tone, location, notes, attendees from events where substr(end, 1, 10) >= ? order by start, id limit ?"

# ---- dates ------------------------------------------------------------------

def days(iso):
    # Days since 1970-01-01 for YYYY-MM-DD (Howard Hinnant's days_from_civil).
    y, m, d = int(iso[0:4]), int(iso[5:7]), int(iso[8:10])
    if m <= 2:
        y -= 1
    era = y // 400
    yoe = y - era * 400
    doy = (153 * ((m + 9) % 12) + 2) // 5 + d - 1
    return era * 146097 + yoe * 365 + yoe // 4 - yoe // 100 + doy - 719468

def iso(n):
    # The inverse of days (civil_from_days).
    z = n + 719468
    era = z // 146097
    doe = z - era * 146097
    yoe = (doe - doe // 1460 + doe // 36524 - doe // 146096) // 365
    y = yoe + era * 400
    doy = doe - (365 * yoe + yoe // 4 - yoe // 100)
    mp = (5 * doy + 2) // 153
    d = doy - (153 * mp + 2) // 5 + 1
    m = mp + 3 if mp < 10 else mp - 9
    if m <= 2:
        y += 1
    return str(y) + "-" + _pad(m) + "-" + _pad(d)

def weekday(n):
    # 0 is Sunday; 1970-01-01 was a Thursday.
    return (n + 4) % 7

def _pad(n):
    # Starlark's % has no width flags.
    return ("0" + str(n))[-2:]

def _days_in(y, m):
    if m == 2:
        return 29 if (y % 4 == 0 and y % 100 != 0) or y % 400 == 0 else 28
    return 30 if m in (4, 6, 9, 11) else 31

def valid_date(s):
    if len(s) != 10 or s[4] != "-" or s[7] != "-" or not (s[0:4] + s[5:7] + s[8:10]).isdigit():
        return False
    y, m, d = int(s[0:4]), int(s[5:7]), int(s[8:10])
    return m >= 1 and m <= 12 and d >= 1 and d <= _days_in(y, m)

def valid_time(s):
    if len(s) != 5 or s[2] != ":" or not (s[0:2] + s[3:5]).isdigit():
        return False
    return int(s[0:2]) < 24 and int(s[3:5]) < 60

def _minutes(hhmm):
    return int(hhmm[0:2]) * 60 + int(hhmm[3:5])

def clock(hhmm):
    # "09:30" -> "9:30", "14:00" -> "14:00": compact, 24-hour.
    h = int(hhmm[0:2])
    return str(h) + ":" + hhmm[3:5]

def _month_start(n):
    s = iso(n)
    return days(s[0:8] + "01")

def _shift_month(n, k):
    s = iso(n)
    t = int(s[0:4]) * 12 + int(s[5:7]) - 1 + k
    y, m = t // 12, t % 12 + 1
    return days(str(y) + "-" + _pad(m) + "-" + _pad(min(int(s[8:10]), _days_in(y, m))))

def _short(n):
    s = iso(n)
    return MONTHS[int(s[5:7]) - 1][0:3] + " " + str(int(s[8:10]))

def long_date(n):
    s = iso(n)
    return WEEKDAYS[weekday(n)] + ", " + MONTHS[int(s[5:7]) - 1] + " " + str(int(s[8:10]))

# ---- events -------------------------------------------------------------------

def _event(r):
    # A row with its span in days and display strings.
    all_day = r["all_day"] == 1
    s, e = days(r["start"][0:10]), days(r["end"][0:10])
    t0 = "" if all_day else r["start"][11:16]
    t1 = "" if all_day else r["end"][11:16]
    when = "All day" if all_day else clock(t0) + " – " + clock(t1)
    if s != e:
        when = _short(s) + " – " + _short(e) + ("" if all_day else " · " + clock(t0))
    people = [p.strip() for p in r["attendees"].split(",") if p.strip()]
    date_label = WEEKDAYS[weekday(s)][0:3] + ", " + _short(s)
    return dict(r, all_day = all_day, s = s, e = e, t0 = t0, t1 = t1, when = when, people = people, date_label = date_label)

def _range(first, last):
    return [_event(r) for r in db.query(Q_RANGE, iso(last), iso(first))]

def _by_day(events, first, last):
    # day number -> the events on that day (all-day first, then by time).
    out = {}
    for ev in events:
        for n in range(max(ev["s"], first), min(ev["e"], last) + 1):
            out.setdefault(n, []).append(ev)
    return out

# ---- views ----------------------------------------------------------------------

def header(state):
    c = days(state["cursor"])
    s = iso(c)
    if state["view"] == "week":
        a = c - weekday(c)
        b = a + 6
        y = iso(b)[0:4]
        return {"title": _short(a) + " – " + _short(b) + ", " + y, "today": days(TODAY)}
    return {"title": MONTHS[int(s[5:7]) - 1] + " " + s[0:4], "today": days(TODAY)}

def month(state):
    # Six weeks from the Sunday on or before the 1st: 42 cells.
    c = days(state["cursor"])
    first = _month_start(c)
    start = first - weekday(first)
    inside = iso(first)[0:7]
    events = _by_day(_range(start, start + 41), start, start + 41)
    t = days(TODAY)
    weeks = []
    for w in range(6):
        row = []
        for i in range(7):
            n = start + w * 7 + i
            evs = events.get(n, [])
            row.append({
                "n": n,
                "date": iso(n),
                "day": int(iso(n)[8:10]),
                "label": long_date(n),
                "inside": iso(n)[0:7] == inside,
                "today": n == t,
                "weekend": i == 0 or i == 6,
                "events": evs[:MONTH_CHIPS] if len(evs) <= MONTH_CHIPS else evs[:MONTH_CHIPS - 1],
                "more": [] if len(evs) <= MONTH_CHIPS else evs,
                "extra": 0 if len(evs) <= MONTH_CHIPS else len(evs) - MONTH_CHIPS + 1,
            })
        weeks.append(row)
    return {"weekdays": [d[0:3] for d in WEEKDAYS], "weeks": weeks}

def _by_start(e):
    # Earliest first; of two starting together, the longer first.
    return (e["m0"], -e["m1"])

def _lanes(evs):
    # Greedy lanes for overlapping timed events: each event takes the first
    # lane free at its start; a cluster of overlaps shares one lane count.
    evs = sorted(evs, key = _by_start)
    ends = []
    cluster = []
    out = []
    cluster_end = -1
    for ev in evs:
        if ev["m0"] >= cluster_end and cluster:
            n = len(ends)
            for x in cluster:
                x["lanes"] = n
            out.extend(cluster)
            cluster, ends = [], []
        lane = -1
        for i, e in enumerate(ends):
            if e <= ev["m0"]:
                lane = i
                break
        if lane < 0:
            lane = len(ends)
            ends.append(ev["m1"])
        else:
            ends[lane] = ev["m1"]
        cluster.append(dict(ev, lane = lane))
        cluster_end = max(cluster_end, ev["m1"])
    n = len(ends)
    for x in cluster:
        x["lanes"] = n
    out.extend(cluster)
    return out

def week(state):
    c = days(state["cursor"])
    a = c - weekday(c)
    events = _by_day(_range(a, a + 6), a, a + 6)
    lo, hi = HOUR_START * 60, HOUR_END * 60
    span = hi - lo
    t = days(TODAY)
    cols = []
    for i in range(7):
        n = a + i
        all_day, timed = [], []
        for ev in events.get(n, []):
            if ev["all_day"] or ev["s"] != ev["e"]:
                all_day.append(ev)
                continue
            m0 = min(max(_minutes(ev["t0"]), lo), hi - 15)
            m1 = min(max(_minutes(ev["t1"]), m0 + 15), hi)
            timed.append(dict(ev, m0 = m0, m1 = m1))
        placed = []
        for ev in _lanes(timed):
            placed.append(dict(ev, top = (ev["m0"] - lo) * 100.0 / span, height = (ev["m1"] - ev["m0"]) * 100.0 / span, short = ev["m1"] - ev["m0"] <= 30))
        cols.append({"n": n, "date": iso(n), "dow": WEEKDAYS[i][0:3], "day": int(iso(n)[8:10]), "today": n == t, "all_day": all_day, "timed": placed})
    hours = [{"h": h, "label": str(h) + ":00"} for h in range(HOUR_START, HOUR_END)]
    now = None
    if t >= a and t <= a + 6:
        now = {"col": t - a}
    return {"days": cols, "hours": hours, "today_col": now}

def agenda(state):
    # The cursor's month, one entry per day that has events.
    c = days(state["cursor"])
    first = _month_start(c)
    last = _shift_month(first, 1) - 1
    events = _by_day(_range(first, last), first, last)
    t = days(TODAY)
    out = []
    for n in range(first, last + 1):
        if n in events:
            out.append({"n": n, "date": iso(n), "label": long_date(n), "dow": WEEKDAYS[weekday(n)][0:3], "day": int(iso(n)[8:10]), "today": n == t, "past": n < t, "events": events[n]})
    return out

def upcoming(state):
    return [_event(r) for r in db.query(Q_UPCOMING, TODAY, 5)]

def tones(state):
    return [{"value": t, "label": TONE_LABELS[t]} for t in TONES]

# ---- actions ------------------------------------------------------------------

def _quiet(state, **kw):
    return dict(state, flash = "", **kw)

def _flash(state, text, **kw):
    return dict(state, flash = text, flashes = state["flashes"] + 1, form_open = False, **kw)

def _draft(date, t0, t1):
    return {"id": 0, "title": "", "all_day": False, "start_date": date, "start_time": t0, "end_date": date, "end_time": t1, "tone": "primary", "location": "", "notes": "", "attendees": ""}

def set_view(state, props, i):
    if i < 0 or i >= len(VIEWS):
        return _quiet(state)
    return _quiet(state, view = VIEWS[i])

def prev(state, props):
    c = days(state["cursor"])
    return _quiet(state, cursor = iso(c - 7 if state["view"] == "week" else _shift_month(c, -1)))

def next(state, props):
    c = days(state["cursor"])
    return _quiet(state, cursor = iso(c + 7 if state["view"] == "week" else _shift_month(c, 1)))

def go_today(state, props):
    return _quiet(state, cursor = TODAY)

def new_event(state, props):
    return _quiet(state, form_open = True, draft = _draft(state["cursor"], "09:00", "10:00"))

def new_on(state, props, n):
    # A click on an empty day: a one-hour event at 09:00 that day.
    return _quiet(state, cursor = iso(n), form_open = True, draft = _draft(iso(n), "09:00", "10:00"))

def new_at(state, props, n, hour):
    # A click on an empty hour in the week view.
    h = min(max(hour, 0), 22)
    return _quiet(state, form_open = True, draft = _draft(iso(n), _pad(h) + ":00", _pad(h + 1) + ":00"))

def edit(state, props, id):
    rows = db.query(Q_ONE, id)
    if not rows:
        return _quiet(state)
    r = rows[0]
    all_day = r["all_day"] == 1
    return _quiet(state, form_open = True, draft = {
        "id": r["id"], "title": r["title"], "all_day": all_day,
        "start_date": r["start"][0:10], "start_time": "09:00" if all_day else r["start"][11:16],
        "end_date": r["end"][0:10], "end_time": "10:00" if all_day else r["end"][11:16],
        "tone": r["tone"], "location": r["location"], "notes": r["notes"], "attendees": r["attendees"],
    })

def close_form(state, props):
    return _quiet(state, form_open = False)

def save(state, props, fields):
    d = {
        "id": state["draft"]["id"],
        "title": fields.get("title", "").strip(),
        "all_day": fields.get("all_day", "") == "on",
        "start_date": fields.get("start_date", "").strip(),
        "start_time": fields.get("start_time", "").strip(),
        "end_date": fields.get("end_date", "").strip(),
        "end_time": fields.get("end_time", "").strip(),
        "tone": fields.get("tone", "primary"),
        "location": fields.get("location", "").strip(),
        "notes": fields.get("notes", "").strip(),
        "attendees": ",".join([p.strip() for p in fields.get("attendees", "").split(",") if p.strip()]),
    }
    errors = {}
    if not d["title"]:
        errors["title"] = "Give the event a title."
    elif len(d["title"]) > MAX_TITLE:
        errors["title"] = "Keep the title under %d characters." % MAX_TITLE
    if not valid_date(d["start_date"]) or (not d["all_day"] and not valid_time(d["start_time"])):
        errors["start"] = "Enter a start date and time."
    if not valid_date(d["end_date"]) or (not d["all_day"] and not valid_time(d["end_time"])):
        errors["end"] = "Enter an end date and time."
    if d["tone"] not in TONES:
        errors["tone"] = "Pick a colour."
    if "start" not in errors and "end" not in errors:
        if d["all_day"]:
            if d["end_date"] < d["start_date"]:
                errors["end"] = "The last day can't be before the first."
        elif d["end_date"] + "T" + d["end_time"] <= d["start_date"] + "T" + d["start_time"]:
            errors["end"] = "The event must end after it starts."
    if errors:
        return [dict(state, draft = d, form_open = True, flash = ""), errors]
    start = d["start_date"] if d["all_day"] else d["start_date"] + "T" + d["start_time"]
    end = d["end_date"] if d["all_day"] else d["end_date"] + "T" + d["end_time"]
    if d["id"]:
        db.exec("update events set title = ?, start = ?, end = ?, all_day = ?, tone = ?, location = ?, notes = ?, attendees = ? where id = ?", d["title"], start, end, 1 if d["all_day"] else 0, d["tone"], d["location"], d["notes"], d["attendees"], d["id"])
        return _flash(state, "Event updated", cursor = d["start_date"])
    db.exec("insert into events (title, start, end, all_day, tone, location, notes, attendees) values (?, ?, ?, ?, ?, ?, ?, ?)", d["title"], start, end, 1 if d["all_day"] else 0, d["tone"], d["location"], d["notes"], d["attendees"])
    return _flash(state, "Event created", cursor = d["start_date"])

def delete(state, props, id):
    db.exec("delete from events where id = ?", id)
    return _flash(state, "Event deleted")
