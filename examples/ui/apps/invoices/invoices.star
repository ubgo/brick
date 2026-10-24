# The Invoices app's data and writes, over the invoices and invoice_lines
# tables (demodb/db_invoices.go). Money is integer cents everywhere; only the editor's text
# fields hold dollars as typed ("12.50"), and parse_money turns them back.
# A tax rate is a percent (10, 7.5); the tax is rounded half up to a cent.

# STATUSES are the closed set of invoices.status, in filter-tab order.
STATUSES = ["draft", "sent", "paid", "overdue"]
LABELS = {"all": "All", "draft": "Draft", "sent": "Sent", "paid": "Paid", "overdue": "Overdue"}
TONES = {"draft": "neutral", "sent": "info", "paid": "success", "overdue": "danger"}

# SORTS are the list orders the sort Select offers: value → label.
SORTS = [
    {"value": "newest", "label": "Newest first"},
    {"value": "oldest", "label": "Oldest first"},
    {"value": "due", "label": "Due soonest"},
    {"value": "amount", "label": "Amount, high to low"},
    {"value": "customer", "label": "Customer, A to Z"},
]

# PER_PAGE rows show on one list page.
PER_PAGE = 8

# TODAY is the demo's clock: fixed so pages render the same bytes on every
# run, and the seed's overdue invoices (demodb/db_invoices.go) are overdue against it.
TODAY = "2026-09-27"

# TERMS is the default days from issue to due for a new invoice.
TERMS = 30

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

def pretty(iso):
    """2026-09-27 → Sep 27, 2026; anything else unchanged."""
    if len(iso) != 10:
        return iso
    return MONTHS[int(iso[5:7], 10) - 1] + " " + str(int(iso[8:10], 10)) + ", " + iso[:4]

def add_days(iso, n):
    return db.query("select date(?, ?) as d", iso, "+" + str(n) + " days")[0]["d"]

def is_date(s):
    if len(s) != 10 or s[4] != "-" or s[7] != "-":
        return False
    if not (s[:4].isdigit() and s[5:7].isdigit() and s[8:].isdigit()):
        return False
    return db.query("select date(?) = ? as ok", s, s)[0]["ok"] == 1

def pad2(n):
    return str(n) if n >= 10 else "0" + str(n)

def dollars(cents):
    """Cents as the text an editor field shows: 1250 → "12.50"."""
    return str(cents // 100) + "." + pad2(cents % 100)

def parse_money(s):
    """"12.50", "12.5", "1,200", "$99.99" → cents; None when it is not money."""
    s = s.strip().replace(",", "").replace("$", "")
    if not s:
        return None
    whole, dot, frac = s.partition(".")
    if not whole:
        whole = "0"
    if not whole.isdigit() or (dot and (not frac.isdigit() or len(frac) > 2)):
        return None
    frac = (frac + "00")[:2]
    return int(whole, 10) * 100 + int(frac, 10)

def parse_rate(s):
    """A tax percent as typed → basis points (7.5 → 750); None when invalid."""
    cents = parse_money(s)
    if cents == None or cents > 5000:
        return None
    return cents

def totals(lines, bp):
    """Subtotal, tax and total in cents for lines [{qty, cents}] at bp basis
    points of tax, the tax rounded half up."""
    sub = 0
    for l in lines:
        sub += l["qty"] * l["cents"]
    tax = (sub * bp + 5000) // 10000
    return {"subtotal": sub, "tax": tax, "total": sub + tax}

def bp_of(rate):
    return int(rate * 100 + 0.5)

def _rows():
    rows = db.query("select i.id, i.number, i.customer, i.email, i.issued, i.due, i.status, i.tax_rate, coalesce(sum(l.qty * l.unit_price), 0) as subtotal from invoices i left join invoice_lines l on l.invoice_id = i.id group by i.id")
    out = []
    for r in rows:
        t = dict(r)
        t["total"] = r["subtotal"] + (r["subtotal"] * bp_of(r["tax_rate"]) + 5000) // 10000
        t["label"] = LABELS[r["status"]]
        t["tone"] = TONES[r["status"]]
        t["issuedLabel"] = pretty(r["issued"])
        t["dueLabel"] = pretty(r["due"])
        out.append(t)
    return out

def _key(sort):
    if sort == "oldest":
        return lambda r: (r["issued"], r["number"])
    if sort == "due":
        return lambda r: (r["due"], r["number"])
    if sort == "amount":
        return lambda r: (-r["total"], r["number"])
    if sort == "customer":
        return lambda r: (r["customer"].lower(), r["number"])
    return lambda r: (r["issued"], r["number"])

def listing(status, q, sort, page):
    """One page of the list for the filters, with the per-status counts and
    sums for the tabs and the stat cards (over the search, not the tab)."""
    q = q.strip().lower()
    rows = [r for r in _rows() if not q or q in r["number"].lower() or q in r["customer"].lower() or q in r["email"].lower()]
    counts = {"all": len(rows)}
    sums = {"all": 0}
    for s in STATUSES:
        counts[s] = 0
        sums[s] = 0
    for r in rows:
        counts[r["status"]] += 1
        sums[r["status"]] += r["total"]
        sums["all"] += r["total"]
    shown = [r for r in rows if status == "all" or r["status"] == status]
    shown = sorted(shown, key = _key(sort), reverse = sort == "newest")
    pages = max(1, (len(shown) + PER_PAGE - 1) // PER_PAGE)
    page = min(max(1, page), pages)
    return {
        "rows": shown[(page - 1) * PER_PAGE:page * PER_PAGE],
        "total": len(shown),
        "page": page,
        "perPage": PER_PAGE,
        "counts": counts,
        "sums": sums,
        "sorts": SORTS,
    }

def get(id):
    """An invoice with its lines and totals, or None."""
    rows = db.query("select id, number, customer, email, issued, due, status, notes, tax_rate from invoices where id = ?", id)
    if not rows:
        return None
    inv = dict(rows[0])
    lines = []
    for l in db.query("select description, qty, unit_price from invoice_lines where invoice_id = ? order by id", id):
        lines.append({"desc": l["description"], "qty": l["qty"], "cents": l["unit_price"], "amount": l["qty"] * l["unit_price"]})
    inv["lines"] = lines
    inv["totals"] = totals(lines, bp_of(inv["tax_rate"]))
    inv["label"] = LABELS[inv["status"]]
    inv["tone"] = TONES[inv["status"]]
    inv["issuedLabel"] = pretty(inv["issued"])
    inv["dueLabel"] = pretty(inv["due"])
    return inv

def customers():
    """Every contact as a Combobox option, the name as the value."""
    return [{"value": r["name"], "label": r["name"] + " · " + r["company"]} for r in db.query("select name, company from contacts order by name")]

def next_number():
    n = db.query("select coalesce(max(cast(substr(number, 5) as integer)), 1000) as n from invoices")[0]["n"] + 1
    s = str(n)
    return "INV-" + "0" * max(0, 4 - len(s)) + s

def validate(d):
    """The editor's fields (all text, as typed) → the clean invoice and the
    errors by field name. Line errors are keyed desc_<i>, qty_<i>, price_<i>."""
    errors = {}
    customer = d["customer"].strip()
    found = db.query("select email from contacts where name = ?", customer) if customer else []
    if not customer:
        errors["customer"] = "Choose a customer."
    elif not found:
        errors["customer"] = "Choose a customer from your contacts."
    issued = d["issued"].strip()
    due = d["due"].strip()
    if not is_date(issued):
        errors["issued"] = "Enter the issue date."
    if not is_date(due):
        errors["due"] = "Enter the due date."
    elif is_date(issued) and due < issued:
        errors["due"] = "The due date cannot be before the issue date."
    if d["invStatus"] not in STATUSES:
        errors["invStatus"] = "Choose a status."
    bp = parse_rate(d["taxRate"])
    if bp == None:
        errors["taxRate"] = "Use a percent from 0 to 50, like 10 or 7.5."
    lines = []
    for i, l in enumerate(d["lines"]):
        desc = l["desc"].strip()
        qty = l["qty"].strip()
        cents = parse_money(l["price"])
        if not desc:
            errors["desc_" + str(i)] = "Describe the item."
        if not qty.isdigit() or int(qty, 10) < 1 or int(qty, 10) > 9999:
            errors["qty_" + str(i)] = "A whole number from 1."
        if cents == None:
            errors["price_" + str(i)] = "A price like 12.50."
        if not (errors.get("desc_" + str(i)) or errors.get("qty_" + str(i)) or errors.get("price_" + str(i))):
            lines.append({"desc": desc, "qty": int(qty, 10), "cents": cents})
    if not d["lines"]:
        errors["lines"] = "Add at least one line."
    if len(d["notes"]) > 500:
        errors["notes"] = "Keep notes under 500 characters."
    inv = {
        "customer": customer,
        "email": found[0]["email"] if found else "",
        "issued": issued,
        "due": due,
        "status": d["invStatus"],
        "notes": d["notes"].strip(),
        "tax_rate": (bp or 0) / 100.0,
        "lines": lines,
    }
    return inv, errors

def save(id, inv):
    """Inserts (id 0) or updates the invoice and replaces its lines; returns
    the id."""
    if id:
        db.exec("update invoices set customer = ?, email = ?, issued = ?, due = ?, status = ?, notes = ?, tax_rate = ? where id = ?", inv["customer"], inv["email"], inv["issued"], inv["due"], inv["status"], inv["notes"], inv["tax_rate"], id)
    else:
        db.exec("insert into invoices (number, customer, email, issued, due, status, notes, tax_rate) values (?, ?, ?, ?, ?, ?, ?, ?)", next_number(), inv["customer"], inv["email"], inv["issued"], inv["due"], inv["status"], inv["notes"], inv["tax_rate"])
        id = db.query("select max(id) as id from invoices")[0]["id"]
    db.exec("delete from invoice_lines where invoice_id = ?", id)
    for l in inv["lines"]:
        db.exec("insert into invoice_lines (invoice_id, description, qty, unit_price) values (?, ?, ?, ?)", id, l["desc"], l["qty"], l["cents"])
    return id

def set_status(id, status):
    if status in STATUSES:
        db.exec("update invoices set status = ? where id = ?", status, id)

def duplicate(id):
    """A draft copy issued today with a new number; returns its id, or 0."""
    inv = get(id)
    if not inv:
        return 0
    return save(0, dict(inv, issued = TODAY, due = add_days(TODAY, TERMS), status = "draft"))

def remove(id):
    db.exec("delete from invoice_lines where invoice_id = ?", id)
    db.exec("delete from invoices where id = ?", id)
