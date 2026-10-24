# The Blog CMS app: the admin (content/apps/cms.html → CmsApp) and the
# public blog (content/apps/cms/blog/*.html → CmsBlogList, CmsPost).
#
# State: CmsApp's state: holds the list's search, filter, sort, page and
# selection, and the editor's two copies of the post being edited:
#   loaded — what the form renders; it changes only when a post is opened
#            or a save returns, so an autosave never re-renders a field
#            the visitor is typing in (or the rich-text editor);
#   draft  — the latest autosaved copy (what Preview shows).
#
# Security: body HTML comes from the Editor in the browser, so it is data.
# It is cleaned with sanitize_html (the site's policy) before it is stored
# or kept in state, and the pages clean it again with {{ sanitize }} when
# they render it.
#
# SQL is fixed text (D8): the ORDER BY is one of six literal queries picked
# by an if-chain, and the filters are always present, switched off by
# their arguments ('all', '').

STATUSES = ["draft", "scheduled", "published"]
COVERS = ["primary", "success", "warning", "danger", "info", "neutral"]
AUTHORS = ["Ada Lovelace", "Grace Hopper", "Alan Turing", "Katherine Johnson"]
PAGE_SIZES = [10, 25, 50]
FIRST_DIR = {"title": "asc", "updated": "desc", "published": "desc"}
MAX_TAGS = 5
MAX_EXCERPT = 200
DEFAULT_TIME = "09:00"

Q_TITLE_ASC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by title collate nocase asc, id asc limit ? offset ?"
Q_TITLE_DESC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by title collate nocase desc, id desc limit ? offset ?"
Q_UPDATED_ASC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by updated asc, id asc limit ? offset ?"
Q_UPDATED_DESC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by updated desc, id desc limit ? offset ?"
Q_PUBLISHED_ASC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by published = '', published asc, id asc limit ? offset ?"
Q_PUBLISHED_DESC = "select id, title, slug, status, tags, author, cover, published, updated from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) order by published = '', published desc, id desc limit ? offset ?"
Q_COUNT = "select count(*) as n from posts where (? = 'all' or status = ?) and (? = '' or title like ? or excerpt like ? or tags like ? or author like ?)"
Q_BY_STATUS = "select status, count(*) as n from posts where (? = '' or title like ? or excerpt like ? or tags like ? or author like ?) group by status"
Q_ONE = "select id, title, slug, excerpt, body_html, status, tags, author, cover, published, updated from posts where id = ?"
Q_SLUG_TAKEN = "select count(*) as n from posts where slug = ? and id != ?"
Q_SLUGS_LIKE = "select slug from posts where slug = ? or slug like ?"
Q_PUBLIC = "select id, title, slug, excerpt, tags, author, cover, published from posts where status = 'published' or (status = 'scheduled' and substr(published, 1, 10) <= ?) order by published desc, id desc limit 100"
Q_PUBLIC_ONE = "select id, title, slug, excerpt, body_html, tags, author, cover, published, updated from posts where slug = ? and (status = 'published' or (status = 'scheduled' and substr(published, 1, 10) <= ?))"

X_INSERT = "insert into posts (title, slug, excerpt, body_html, status, tags, author, cover, published, updated) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
X_UPDATE = "update posts set title = ?, slug = ?, excerpt = ?, body_html = ?, status = ?, tags = ?, author = ?, cover = ?, published = ?, updated = ? where id = ?"
X_DELETE = "delete from posts where id = ?"
X_STATUS = "update posts set status = ?, published = ?, updated = ? where id = ?"

EMPTY = {"title": "", "slug": "", "excerpt": "", "body": "", "tags": "", "status": "draft", "author": "Ada Lovelace", "cover": "primary", "date": "", "time": DEFAULT_TIME}

# ---- helpers ---------------------------------------------------------------

def slugify(text):
    """A title as a slug: lowercase ASCII letters and digits joined by single
    hyphens (the rule demodb/db_cms.go's seed uses too)."""
    out = ""
    dash = False
    for c in text.lower().elems():
        if (c >= "a" and c <= "z") or (c >= "0" and c <= "9"):
            out += c
            dash = False
        elif out and not dash:
            out += "-"
            dash = True
    return out.rstrip("-")

def _slug_ok(slug):
    if not slug or slug.startswith("-") or slug.endswith("-") or "--" in slug or len(slug) > 80:
        return False
    for c in slug.elems():
        if not ((c >= "a" and c <= "z") or (c >= "0" and c <= "9") or c == "-"):
            return False
    return True

def _tags(text):
    out = []
    for t in text.split(","):
        t = t.strip().lower()
        if t and t not in out:
            out.append(t)
    return out

def _text_of(html):
    # The visible text of sanitized HTML, roughly: enough to tell an empty
    # body (just <p></p>) from a real one.
    out = ""
    inside = False
    for c in html.elems():
        if c == "<":
            inside = True
        elif c == ">":
            inside = False
        elif not inside:
            out += c
    return out.strip()

# The cover picker's swatches: each tone's light-theme colour (what the
# swatch shows; ColorPicker's colours are data) and back. The cover itself
# is drawn from the tone's token, so it follows dark mode and accents.
SWATCHES = {"primary": "#4f46e5", "info": "#0284c7", "success": "#16a34a", "warning": "#d97706", "danger": "#dc2626", "neutral": "#71717a"}

def swatch(tone):
    return SWATCHES.get(tone, SWATCHES["primary"])

def _cover(value):
    for tone, colour in SWATCHES.items():
        if value.lower() == colour:
            return tone
    return value

def _fields(fields):
    """The editor's submitted fields, trimmed, with the body sanitized."""
    title = fields.get("title", "").strip()
    slug = fields.get("slug", "").strip().lower()
    return {
        "title": title,
        "slug": slug if slug else slugify(title),
        "excerpt": fields.get("excerpt", "").strip(),
        "body": sanitize_html(fields.get("body", "")),
        "tags": ",".join(_tags(fields.get("tags", ""))),
        "status": fields.get("status", "draft"),
        "author": fields.get("author", AUTHORS[0]),
        "cover": _cover(fields.get("cover", "primary")),
        "date": fields.get("date", "").strip(),
        "time": fields.get("time", "").strip() or DEFAULT_TIME,
    }

def _validate(f, id, publishing):
    errors = {}
    if len(f["title"]) < 3:
        errors["title"] = "Give the post a title of at least three characters."
    elif len(f["title"]) > 120:
        errors["title"] = "Keep the title under 120 characters."
    if not _slug_ok(f["slug"]):
        errors["slug"] = "Use lowercase letters, digits and single hyphens."
    elif db.query(Q_SLUG_TAKEN, f["slug"], id)[0]["n"] > 0:
        errors["slug"] = "Another post already uses this address."
    if len(f["excerpt"]) > MAX_EXCERPT:
        errors["excerpt"] = "Keep the excerpt under %d characters." % MAX_EXCERPT
    tags = _tags(f["tags"])
    if len(tags) > MAX_TAGS:
        errors["tags"] = "Use at most %d tags." % MAX_TAGS
    for t in tags:
        if not _slug_ok(t) or len(t) > 24:
            errors["tags"] = "Tags are short words: letters, digits and hyphens."
    if f["status"] not in STATUSES:
        errors["status"] = "Choose draft, scheduled or published."
    if f["author"] not in AUTHORS:
        errors["author"] = "Choose an author from the list."
    if f["cover"] not in COVERS:
        errors["cover"] = "Choose a cover colour."
    if publishing and not _text_of(f["body"]):
        errors["body"] = "Write something before it goes live."
    if f["status"] == "scheduled":
        if len(f["date"]) != 10:
            errors["date"] = "A scheduled post needs a publish date."
        elif f["date"] <= today():
            errors["date"] = "Pick a date after today, or publish now."
    if f["time"] and (len(f["time"]) != 5 or f["time"][2] != ":"):
        errors["time"] = "Use a time like 09:30."
    return errors

def _published(f):
    # When the post goes (or went) live: the chosen date and time, or today.
    if f["status"] == "draft":
        return ""
    return (f["date"] or today()) + "T" + f["time"]

def _write(f, id):
    """Insert or update the post; returns its id."""
    if id:
        db.exec(X_UPDATE, f["title"], f["slug"], f["excerpt"], f["body"], f["status"], f["tags"], f["author"], f["cover"], _published(f), today(), id)
        return id
    db.exec(X_INSERT, f["title"], f["slug"], f["excerpt"], f["body"], f["status"], f["tags"], f["author"], f["cover"], _published(f), today())
    return db.query("select id from posts where slug = ?", f["slug"])[0]["id"]

def _form_of(p):
    return {
        "title": p["title"], "slug": p["slug"], "excerpt": p["excerpt"], "body": p["body_html"],
        "tags": p["tags"], "status": p["status"], "author": p["author"], "cover": p["cover"],
        "date": p["published"][:10], "time": p["published"][11:16] or DEFAULT_TIME,
    }

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

def _date(iso):
    # "2026-05-07" -> "7 May 2026", as the pages show dates.
    y, m, d = iso[:10].split("-")
    return "%d %s %s" % (int(d), MONTHS[int(m) - 1], y)

def _flash(state, text, variant = "success"):
    # Each flash gets a new id (the counter), which the Toaster shows once.
    return dict(state, toast = text, toastVariant = variant, toasts = state["toasts"] + 1)

def _quiet(state):
    return dict(state, toast = "")

# ---- the list --------------------------------------------------------------

def _filters(state):
    q = state["q"].strip()
    like = "%" + q + "%"
    status = state["status"] if state["status"] in STATUSES else "all"
    return [status, status, q, like, like, like, like], q

def _per(state):
    return state["per"] if state["per"] in PAGE_SIZES else PAGE_SIZES[0]

def _rows(key, args):
    if key == "title_asc":
        return db.query(Q_TITLE_ASC, *args)
    if key == "title_desc":
        return db.query(Q_TITLE_DESC, *args)
    if key == "updated_asc":
        return db.query(Q_UPDATED_ASC, *args)
    if key == "published_asc":
        return db.query(Q_PUBLISHED_ASC, *args)
    if key == "published_desc":
        return db.query(Q_PUBLISHED_DESC, *args)
    return db.query(Q_UPDATED_DESC, *args)

def view(state):
    """What the list shows, from the database."""
    filt, q = _filters(state)
    total = db.query(Q_COUNT, *filt)[0]["n"]
    per = _per(state)
    pages = max(1, (total + per - 1) // per)
    page = state["page"] if state["pageq"] == q else 1
    page = min(max(1, page), pages)
    sort = state["sort"] if state["sort"] in FIRST_DIR else "updated"
    dir = "asc" if state["dir"] == "asc" else "desc"
    rows = _rows(sort + "_" + dir, filt + [per, (page - 1) * per])
    for r in rows:
        r["selected"] = r["id"] in state["selected"]
        r["taglist"] = _tags(r["tags"])
        r["live"] = r["status"] == "published" or (r["status"] == "scheduled" and r["published"][:10] <= today())
    counts = {"all": 0, "draft": 0, "scheduled": 0, "published": 0}
    for c in db.query(Q_BY_STATUS, q, "%" + q + "%", "%" + q + "%", "%" + q + "%", "%" + q + "%"):
        counts[c["status"]] = c["n"]
        counts["all"] += c["n"]
    return {
        "rows": rows, "total": total, "page": page, "per": per, "sort": sort, "dir": dir, "counts": counts,
        "allSelected": len(rows) > 0 and all([r["selected"] for r in rows]),
        "someSelected": any([r["selected"] for r in rows]) and not all([r["selected"] for r in rows]),
        "today": today(),
    }

def filter_status(state, props, status):
    return dict(_quiet(state), status = status if status in STATUSES else "all", page = 1, pageq = state["q"].strip(), selected = [])

def sort_by(state, props, key):
    if key not in FIRST_DIR:
        return state
    dir = FIRST_DIR[key]
    if state["sort"] == key:
        dir = "desc" if state["dir"] == "asc" else "asc"
    return dict(_quiet(state), sort = key, dir = dir, page = 1, pageq = state["q"].strip())

def goto(state, props, page):
    return dict(_quiet(state), page = int(page), pageq = state["q"].strip())

def per_page(state, props, value):
    n = int(value) if str(value).isdigit() else PAGE_SIZES[0]
    return dict(_quiet(state), per = n if n in PAGE_SIZES else PAGE_SIZES[0], page = 1, pageq = state["q"].strip())

def clear_filters(state, props):
    return dict(_quiet(state), q = "", status = "all", page = 1, pageq = "")

def toggle(state, props, id, checked):
    sel = [i for i in state["selected"] if i != id]
    if checked:
        sel.append(id)
    return dict(_quiet(state), selected = sel)

def toggle_page(state, props, checked):
    ids = [r["id"] for r in view(state)["rows"]]
    sel = [i for i in state["selected"] if i not in ids]
    if checked:
        sel = sel + ids
    return dict(_quiet(state), selected = sel)

def clear_selection(state, props):
    return dict(_quiet(state), selected = [])

def _set_status(id, status):
    found = db.query(Q_ONE, id)
    if not found:
        return None
    p = found[0]
    if status == "published":
        body = _text_of(p["body_html"])
        if not body:
            return None
        when = p["published"] if p["published"] and p["published"][:10] <= today() else today() + "T" + DEFAULT_TIME
    else:
        when = ""
    db.exec(X_STATUS, status, when, today(), id)
    return p["title"]

def publish(state, props, id):
    title = _set_status(id, "published")
    if title == None:
        return _flash(state, "That post cannot be published: it is gone or empty.", "danger")
    return _flash(state, "Published “%s”." % title)

def unpublish(state, props, id):
    title = _set_status(id, "draft")
    if title == None:
        return _flash(state, "That post no longer exists.", "danger")
    return _flash(state, "Moved “%s” back to drafts." % title)

def publish_selected(state, props):
    n = len([1 for id in state["selected"] if _set_status(id, "published") != None])
    return _flash(dict(state, selected = []), "Published %d post%s." % (n, "" if n == 1 else "s"))

def unpublish_selected(state, props):
    n = len([1 for id in state["selected"] if _set_status(id, "draft") != None])
    return _flash(dict(state, selected = []), "Moved %d post%s to drafts." % (n, "" if n == 1 else "s"))

def remove_selected(state, props):
    n = 0
    for id in state["selected"]:
        db.exec(X_DELETE, id)
        n += 1
    return _flash(dict(state, selected = []), "Deleted %d post%s." % (n, "" if n == 1 else "s"))

def remove(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        return _flash(dict(state, view = "list"), "That post was already deleted.", "warning")
    db.exec(X_DELETE, id)
    return _flash(dict(state, view = "list", post = 0, selected = [i for i in state["selected"] if i != id]), "Deleted “%s”." % found[0]["title"])

def duplicate(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        return _flash(state, "That post no longer exists.", "danger")
    f = _form_of(found[0])
    base = f["slug"] + "-copy"
    taken = [r["slug"] for r in db.query(Q_SLUGS_LIKE, base, base + "-%")]
    slug = base
    n = 2
    for _ in range(100):
        if slug not in taken:
            break
        slug = base + "-%d" % n
        n += 1
    f = dict(f, title = f["title"] + " (copy)", slug = slug, status = "draft", date = "")
    _write(f, 0)
    return _flash(state, "Created a draft copy of “%s”." % found[0]["title"])

# ---- the editor --------------------------------------------------------------

def new_post(state, props):
    return dict(_quiet(state), view = "edit", post = 0, loaded = dict(EMPTY), draft = dict(EMPTY), note = "New post — not saved yet", noteTone = "muted", autosaves = 0)

def edit(state, props, id):
    found = db.query(Q_ONE, id)
    if not found:
        return _flash(state, "That post no longer exists.", "danger")
    f = _form_of(found[0])
    return dict(_quiet(state), view = "edit", post = id, loaded = f, draft = f, note = "Last saved " + _date(found[0]["updated"]), noteTone = "muted", autosaves = 0)

def back(state, props):
    return dict(_quiet(state), view = "list", post = 0)

def autosave(state, props, fields):
    """Keeps the editor's latest copy on the server as the visitor types. A
    draft (or a new post) is written to the database as a draft; a live post
    changes only when the visitor saves, so its edits wait in the session."""
    f = _fields(fields)
    new = dict(state, draft = f, autosaves = state["autosaves"] + 1, toast = "")
    live = state["post"] and db.query(Q_ONE, state["post"]) and db.query(Q_ONE, state["post"])[0]["status"] != "draft"
    if live:
        return dict(new, note = "Unsaved changes — Update to publish them", noteTone = "warning")
    errors = _validate(dict(f, status = "draft"), state["post"], False)
    if errors:
        first = errors.values()[0]
        return dict(new, note = "Draft not saved: " + first, noteTone = "danger")
    id = _write(dict(f, status = "draft"), state["post"])
    return dict(new, post = id, note = "Draft saved", noteTone = "success")

def save(state, props, fields):
    """Save, update, schedule or publish: intent=publish forces published."""
    f = _fields(fields)
    if fields.get("intent", "") == "publish":
        f = dict(f, status = "published")
    publishing = f["status"] != "draft"
    errors = _validate(f, state["post"], publishing)
    if errors:
        return [dict(_quiet(state), loaded = f, draft = f, note = "Fix the highlighted fields", noteTone = "danger"), errors]
    was = db.query(Q_ONE, state["post"])[0]["status"] if state["post"] and db.query(Q_ONE, state["post"]) else ""
    id = _write(f, state["post"])
    saved = db.query(Q_ONE, id)[0]
    f = _form_of(saved)
    new = dict(state, post = id, loaded = f, draft = f, note = "Saved", noteTone = "success")
    if f["status"] == "published" and was != "published":
        return _flash(new, "Published “%s”." % f["title"])
    if f["status"] == "scheduled":
        return _flash(new, "Scheduled “%s” for %s at %s." % (f["title"], _date(f["date"]), f["time"]))
    return _flash(new, "Saved “%s”." % f["title"])

# ---- the public blog (loaders) ---------------------------------------------------

def public_slugs():
    # brick: {paths:} of content/apps/cms/blog/[slug].html: one route per
    # live post, for the closed world in serve and for static export.
    return [{"slug": r["slug"]} for r in db.query(Q_PUBLIC, today())]

def post_page(input):
    # The dynamic post page's loader: a slug that is not live is a 404.
    slug = input["page"]["route"]["slug"]
    if not _slug_ok(slug) or not db.query(Q_PUBLIC_ONE, slug, today()):
        brick.not_found()
    return {}

def published_posts(input):
    rows = db.query(Q_PUBLIC, today())
    for r in rows:
        r["taglist"] = _tags(r["tags"])
    return {"posts": rows}

def public_post(input):
    # The slug comes from the dynamic route ([slug].html passes it as a
    # prop) or, on the older post page, from ?slug= in the query.
    slug = str(input["props"].get("slug") or input["ctx"].get("query", {}).get("slug", "")).strip().lower()
    found = db.query(Q_PUBLIC_ONE, slug, today()) if _slug_ok(slug) else []
    if not found:
        return {"post": {}, "slug": slug, "more": []}
    p = found[0]
    p["taglist"] = _tags(p["tags"])
    more = [r for r in db.query(Q_PUBLIC, today()) if r["id"] != p["id"]][:3]
    return {"post": p, "slug": slug, "more": more}
