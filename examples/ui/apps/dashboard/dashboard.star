# The Dashboard app's data: every number comes from the seeded orders table
# (demodb/db_dashboard.go writes the same 120 orders on every run). The dashboard's "today"
# is the newest order's date, not the clock, so the page renders the same
# bytes on every run and a range always has data. Amounts are in cents.

# RANGES are the date-range choices, in days.
RANGES = {"7d": {"days": 7}, "30d": {"days": 30}, "90d": {"days": 90}}

# AVG is the revenue line's rolling window in days: the store takes about
# one order a day, so raw daily revenue is a row of spikes; a 7-day average
# shows the trend and is still exact arithmetic over the table.
AVG = 7

# PER_PAGE is the recent-orders table's page size.
PER_PAGE = 8

MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

# STATUS names each order status's label and tone (Badge and chart tones).
STATUS = {"paid": {"label": "Paid", "tone": "success"}, "pending": {"label": "Pending", "tone": "warning"}, "refunded": {"label": "Refunded", "tone": "danger"}}

def short(iso):
    """Sep 2 from 2026-09-02."""
    return MONTHS[int(iso[5:7], 10) - 1] + " " + str(int(iso[8:10], 10))

def signed(x):
    """x rounded to one decimal with an explicit sign: +3.2 or -1.0.
    (Starlark's % has no precision, so the rounding is by hand.)"""
    n = int((x if x >= 0 else -x) * 10 + 0.5)
    return ("+" if x >= 0 else "-") + str(n // 10) + "." + str(n % 10)

def shift(iso, days):
    return db.query("select date(?, ?) as d", iso, ("+" if days >= 0 else "") + str(days) + " days")[0]["d"]

def total(rows, key):
    n = 0
    for r in rows:
        n += r[key]
    return n

def period(start, end):
    """The KPI inputs for placed dates start..end inclusive."""
    r = db.query("select count(*) as orders, coalesce(sum(case when status = 'paid' then amount else 0 end), 0) as cents, coalesce(sum(case when status = 'paid' then 1 else 0 end), 0) as paid, coalesce(sum(case when status = 'refunded' then 1 else 0 end), 0) as refunded from orders where placed between ? and ?", start, end)[0]
    aov = r["cents"] / r["paid"] / 100 if r["paid"] else 0
    rate = r["refunded"] * 100 / r["orders"] if r["orders"] else 0
    return {"revenue": r["cents"] // 100, "orders": r["orders"], "aov": aov, "refundRate": rate}

def trend(delta):
    """flat for a zero change, so the Stat draws no arrow and no colour; "" lets it read the sign."""
    return "flat" if delta.startswith("+0.0") else ""

def pct(now, before):
    """The change from before to now as a signed percent string, or "" when there is nothing to compare."""
    if not before:
        return ""
    return signed((now - before) * 100 / before) + "%"

def board(input):
    props = input["props"]
    rng = RANGES.get(props["range"], RANGES["30d"])
    days = rng["days"]
    end = db.query("select max(placed) as d from orders")[0]["d"]
    start = shift(end, -(days - 1))
    prev_end = shift(start, -1)
    prev_start = shift(prev_end, -(days - 1))
    now = period(start, end)
    before = period(prev_start, prev_end)
    has_prev = db.query("select count(*) as n from orders where placed between ? and ?", prev_start, prev_end)[0]["n"] > 0
    kpis = [
        {"label": "Revenue", "value": now["revenue"], "money": True, "delta": pct(now["revenue"], before["revenue"]) if has_prev else "", "icon": "dollar", "invert": False},
        {"label": "Orders", "value": now["orders"], "money": False, "delta": pct(now["orders"], before["orders"]) if has_prev else "", "icon": "shopping-cart", "invert": False},
        {"label": "Avg. order value", "value": now["aov"], "money": True, "delta": pct(now["aov"], before["aov"]) if has_prev else "", "icon": "trending-up", "invert": False},
        {"label": "Refund rate", "value": now["refundRate"], "percent": True, "delta": (signed(now["refundRate"] - before["refundRate"]) + " pts") if has_prev else "", "icon": "refresh", "invert": True},
    ]
    for k in kpis:
        k["trend"] = trend(k["delta"])

    daily = db.query("with recursive d(day) as (select ? union all select date(day, '+1 day') from d where day < ?) select d.day as day, coalesce(sum(case when o.status = 'paid' then o.amount else 0 end), 0) as cents from d left join orders o on o.placed = d.day group by d.day order by d.day", shift(start, -(AVG - 1)), end)
    revenue = []
    for i in range(AVG - 1, len(daily)):
        window = daily[i - AVG + 1:i + 1]
        revenue.append({"label": short(daily[i]["day"]), "revenue": total(window, "cents") // AVG // 100})

    status = [{"label": STATUS[r["status"]]["label"], "value": r["n"], "tone": STATUS[r["status"]]["tone"]} for r in db.query("select status, count(*) as n from orders where placed between ? and ? group by status order by n desc, status", start, end)]
    products = db.query("select product, sum(amount) / 100 as revenue from orders where status = 'paid' and placed between ? and ? group by product order by revenue desc, product limit 5", start, end)

    count = now["orders"]
    pages = max(1, (count + PER_PAGE - 1) // PER_PAGE)
    page = min(max(1, props["page"]), pages)
    rows = db.query("select id, customer, product, amount, status, placed from orders where placed between ? and ? order by placed desc, id desc limit ? offset ?", start, end, PER_PAGE, (page - 1) * PER_PAGE)
    orders = [{"id": "#%d" % (1000 + r["id"]), "customer": r["customer"], "product": r["product"], "amount": r["amount"] / 100, "status": STATUS[r["status"]]["label"], "tone": STATUS[r["status"]]["tone"], "date": short(r["placed"])} for r in rows]

    verbs = {"paid": "paid for", "pending": "ordered", "refunded": "was refunded for"}
    icons = {"paid": "check", "pending": "clock", "refunded": "refresh"}
    activity = [{"title": r["customer"] + " " + verbs[r["status"]] + " " + r["product"], "time": short(r["placed"]), "datetime": r["placed"], "icon": icons[r["status"]], "tone": STATUS[r["status"]]["tone"]} for r in db.query("select customer, product, status, placed from orders where placed between ? and ? order by placed desc, id desc limit 5", start, end)]

    return {"kpis": kpis, "revenue": revenue, "status": status, "products": products, "orders": orders, "page": page, "total": count, "perPage": PER_PAGE, "start": short(start), "end": short(end), "activity": activity}
