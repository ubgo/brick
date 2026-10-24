# The Contacts app (content/apps/contacts.html, apps/contacts/ContactsApp.html):
# the view query and every action of the ContactsApp stateful component.
#
# All state lives on the server in ContactsApp's state: (search, filter,
# sort, page, selection, the open form and the open record). The template
# calls view(state) for the rows it shows; an action gets (state, props,
# args…), changes the database with db.exec, and returns the new state, or
# [state, errors] when a form does not validate.
#
# What an action wants the browser to do besides re-rendering is an effect
#: brick.flash shows a message in the kit Toaster, and
# brick.dispatch tells every other component on the page that something
# happened ("contact-added", "contact-deleted"), so ContactCount in the
# topbar refreshes without this module knowing it exists.
#
# SQL is fixed text (D8). A dynamic ORDER BY is one of six literal queries
# chosen by an if-chain, and the filters are always present, switched off by
# their arguments ('all', '').

STATUSES = ["active", "lead", "churned"]
PAGE_SIZES = [10, 25, 50]

# Default direction when a column is first sorted: text ascends, dates show
# the newest first.
FIRST_DIR = {"name": "asc", "company": "asc", "created": "desc"}

Q_NAME_ASC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by name collate nocase asc, id asc limit ? offset ?"
Q_NAME_DESC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by name collate nocase desc, id desc limit ? offset ?"
Q_COMPANY_ASC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by company collate nocase asc, name collate nocase asc limit ? offset ?"
Q_COMPANY_DESC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by company collate nocase desc, name collate nocase asc limit ? offset ?"
Q_CREATED_ASC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by created asc, id asc limit ? offset ?"
Q_CREATED_DESC = "select id, name, email, company, role, status, created from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by created desc, id desc limit ? offset ?"
Q_COUNT = "select count(*) as n from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?)"
Q_BY_STATUS = "select status, count(*) as n from contacts where (? = '' or name like ? or email like ? or company like ? or role like ?) group by status"
Q_ALL = "select count(*) as n from contacts"
Q_ALL_ACTIVE = "select count(*) as n from contacts where status = 'active'"
Q_ONE = "select id, name, email, company, role, status, created from contacts where id = ?"
Q_EMAIL_TAKEN = "select count(*) as n from contacts where lower(email) = lower(?) and id != ?"
Q_IDS = "select id from contacts where (? = 'all' or status = ?) and (? = '' or name like ? or email like ? or company like ? or role like ?) order by id limit 1000"

X_INSERT = "insert into contacts (name, email, company, role, status, created) values (?, ?, ?, ?, ?, ?)"
X_UPDATE = "update contacts set name = ?, email = ?, company = ?, role = ?, status = ? where id = ?"
X_DELETE = "delete from contacts where id = ?"
X_ACTIVATE = "update contacts set status = 'active' where id = ?"

EMPTY_FORM = {"open": False, "id": 0, "name": "", "email": "", "company": "", "role": "", "status": "lead"}

def _filters(state):
    q = state["q"].strip()
    like = "%" + q + "%"
    status = state["status"] if state["status"] in STATUSES else "all"
    return [status, status, q, like, like, like, like], q

def _per(state):
    return state["per"] if state["per"] in PAGE_SIZES else PAGE_SIZES[0]

def _page(state, total):
    # A page number belongs to the search it was chosen for: typing a new
    # search (a $set, which runs no action) starts again at page 1.
    per = _per(state)
    pages = max(1, (total + per - 1) // per)
    page = state["page"] if state["pageq"] == state["q"].strip() else 1
    return min(max(1, page), pages), pages

def _rows(key, args):
    if key == "name_asc":
        return db.query(Q_NAME_ASC, *args)
    if key == "name_desc":
        return db.query(Q_NAME_DESC, *args)
    if key == "company_asc":
        return db.query(Q_COMPANY_ASC, *args)
    if key == "company_desc":
        return db.query(Q_COMPANY_DESC, *args)
    if key == "created_asc":
        return db.query(Q_CREATED_ASC, *args)
    return db.query(Q_CREATED_DESC, *args)

def view(state):
    """Everything the page shows that comes from the database."""
    filt, q = _filters(state)
    total = db.query(Q_COUNT, *filt)[0]["n"]
    page, pages = _page(state, total)
    per = _per(state)
    sort = state["sort"] if state["sort"] in FIRST_DIR else "created"
    dir = "asc" if state["dir"] == "asc" else "desc"
    rows = _rows(sort + "_" + dir, filt + [per, (page - 1) * per])
    selected = state["selected"]
    for r in rows:
        r["selected"] = r["id"] in selected
    counts = {"all": 0, "active": 0, "lead": 0, "churned": 0}
    for c in db.query(Q_BY_STATUS, q, "%" + q + "%", "%" + q + "%", "%" + q + "%", "%" + q + "%"):
        counts[c["status"]] = c["n"]
        counts["all"] += c["n"]
    all_selected = len(rows) > 0 and all([r["selected"] for r in rows])
    some_selected = any([r["selected"] for r in rows]) and not all_selected
    detail = {}
    if state["detail"]:
        found = db.query(Q_ONE, state["detail"])
        if found:
            detail = found[0]
    return {
        "rows": rows, "total": total, "page": page, "pages": pages, "per": per,
        "sort": sort, "dir": dir, "counts": counts, "everyone": db.query(Q_ALL)[0]["n"], "everyoneActive": db.query(Q_ALL_ACTIVE)[0]["n"],
        "allSelected": all_selected, "someSelected": some_selected, "detail": detail,
        "activity": _activity(detail) if detail else [],
    }

def _activity(c):
    # Fake but deterministic history for the detail drawer.
    return [
        {"title": "Status is " + c["status"], "time": "Today", "icon": "check-circle" if c["status"] == "active" else "clock", "tone": {"active": "success", "lead": "info", "churned": "danger"}[c["status"]]},
        {"title": "E-mailed about the Q3 renewal", "time": "3 days ago", "icon": "mail", "tone": "neutral"},
        {"title": "Call with " + c["name"].split(" ")[0] + " — 25 min", "time": "Last week", "icon": "phone", "tone": "primary", "body": "Walked through pricing; wants a demo for the " + c["company"] + " team."},
        {"title": "Added to Contacts", "time": _date(c["created"]), "datetime": c["created"], "icon": "plus", "tone": "neutral"},
    ]

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

def _date(iso):
    # "2026-05-07" -> "7 May 2026", as the table shows dates.
    y, m, d = iso.split("-")
    return "%d %s %s" % (int(d), MONTHS[int(m) - 1], y)

def total():
    """How many contacts the workspace has, for ContactCount."""
    return db.query(Q_ALL)[0]["n"]

# ---- list: filter, sort, page, select ------------------------------------

def filter_status(state, props, status):
    return dict(state, status = status if status in STATUSES else "all", page = 1, pageq = state["q"].strip())

def sort_by(state, props, key):
    if key not in FIRST_DIR:
        return state
    dir = FIRST_DIR[key]
    if state["sort"] == key:
        dir = "desc" if state["dir"] == "asc" else "asc"
    return dict(state, sort = key, dir = dir, page = 1, pageq = state["q"].strip())

def goto(state, props, page):
    return dict(state, page = int(page), pageq = state["q"].strip())

def per_page(state, props, value):
    n = int(value) if str(value).isdigit() else PAGE_SIZES[0]
    return dict(state, per = n if n in PAGE_SIZES else PAGE_SIZES[0], page = 1, pageq = state["q"].strip())

def clear_filters(state, props):
    return dict(state, q = "", status = "all", page = 1, pageq = "")

def toggle(state, props, id, checked):
    sel = [i for i in state["selected"] if i != id]
    if checked:
        sel.append(id)
    return dict(state, selected = sel)

def toggle_page(state, props, checked):
    ids = [r["id"] for r in view(state)["rows"]]
    sel = [i for i in state["selected"] if i not in ids]
    if checked:
        sel = sel + ids
    return dict(state, selected = sel)

def select_matching(state, props):
    filt, _ = _filters(state)
    return dict(state, selected = [r["id"] for r in db.query(Q_IDS, *filt)])

def clear_selection(state, props):
    return dict(state, selected = [])

# ---- create, edit, delete ------------------------------------------------

def new_contact(state, props):
    return dict(state, form = dict(EMPTY_FORM, open = True))

def edit(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        brick.flash("That contact no longer exists.", "danger")
        return state
    c = found[0]
    return dict(state, form = {"open": True, "id": c["id"], "name": c["name"], "email": c["email"], "company": c["company"], "role": c["role"], "status": c["status"]})

def duplicate(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        brick.flash("That contact no longer exists.", "danger")
        return state
    c = found[0]
    return dict(state, form = {"open": True, "id": 0, "name": c["name"] + " (copy)", "email": "", "company": c["company"], "role": c["role"], "status": c["status"]})

def close_form(state, props):
    return dict(state, form = dict(EMPTY_FORM))

def _valid_email(email):
    at = email.find("@")
    return at > 0 and "." in email[at + 1:] and not email.endswith(".") and " " not in email and email.count("@") == 1

def save(state, props, fields):
    id = state["form"]["id"]
    f = {
        "open": True, "id": id,
        "name": fields.get("name", "").strip(),
        "email": fields.get("email", "").strip().lower(),
        "company": fields.get("company", "").strip(),
        "role": fields.get("role", "").strip(),
        "status": fields.get("status", ""),
    }
    errors = {}
    if len(f["name"]) < 2:
        errors["name"] = "Enter a name of at least two letters."
    elif len(f["name"]) > 80:
        errors["name"] = "Keep the name under 80 characters."
    if not _valid_email(f["email"]):
        errors["email"] = "Enter an e-mail address like ada@example.com."
    elif db.query(Q_EMAIL_TAKEN, f["email"], id)[0]["n"] > 0:
        errors["email"] = "Another contact already uses this e-mail."
    if f["status"] not in STATUSES:
        errors["status"] = "Choose active, lead or churned."
    if errors:
        return [dict(state, form = f), errors]
    if id:
        db.exec(X_UPDATE, f["name"], f["email"], f["company"], f["role"], f["status"], id)
        brick.flash("Saved changes to " + f["name"] + ".", "success")
        return dict(state, form = dict(EMPTY_FORM))
    db.exec(X_INSERT, f["name"], f["email"], f["company"], f["role"], f["status"], today())
    brick.dispatch("contact-added", {"email": f["email"]})
    brick.flash("Added " + f["name"] + " to your contacts.", "success")
    # Show the new contact: newest first, first page, no filter hiding it.
    return dict(state, form = dict(EMPTY_FORM), q = "", status = "all", sort = "created", dir = "desc", page = 1, pageq = "")

def remove(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        brick.flash("That contact was already deleted.", "warning")
        return state
    db.exec(X_DELETE, id)
    brick.dispatch("contact-deleted", {"ids": [id]})
    brick.flash("Deleted " + found[0]["name"] + ".", "success")
    return dict(state, selected = [i for i in state["selected"] if i != id], detail = 0 if state["detail"] == id else state["detail"])

def remove_selected(state, props):
    n = 0
    for id in state["selected"]:
        db.exec(X_DELETE, id)
        n += 1
    if not n:
        return state
    # One event for the whole batch, carrying ids, so each listener
    # refreshes once rather than once per row.
    brick.dispatch("contact-deleted", {"ids": state["selected"]})
    brick.flash("Deleted %d contact%s." % (n, "" if n == 1 else "s"), "success")
    return dict(state, selected = [], detail = 0 if state["detail"] in state["selected"] else state["detail"])

def activate_selected(state, props):
    n = 0
    for id in state["selected"]:
        db.exec(X_ACTIVATE, id)
        n += 1
    if not n:
        return state
    brick.flash("Marked %d contact%s active." % (n, "" if n == 1 else "s"), "success")
    return dict(state, selected = [])

# ---- detail drawer -------------------------------------------------------

def show(state, props, id):
    return dict(state, detail = id)

def close_detail(state, props):
    return dict(state, detail = 0)
