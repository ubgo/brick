# Loaders for the Charts demos: aggregates over the showcase's orders
# table (db.go seeds the same 120 orders on every run, so the charts render
# the same bytes every time). Amounts are stored in cents.

# months names the month part of an ISO date, for the x axis.
months = {"01": "Jan", "02": "Feb", "03": "Mar", "04": "Apr", "05": "May", "06": "Jun", "07": "Jul", "08": "Aug", "09": "Sep", "10": "Oct", "11": "Nov", "12": "Dec"}

# status_tones gives each order status its semantic tone: the colour
# follows what the status means, not its position in the list.
status_tones = {"paid": "success", "pending": "warning", "refunded": "danger"}

def revenue_by_month(input):
    rows = db.query("select substr(placed, 1, 7) as month, sum(amount) / 100 as revenue, count(*) as orders from orders where status = 'paid' group by month order by month")
    return {"data": [{"month": months[r["month"][5:7]], "revenue": r["revenue"], "orders": r["orders"]} for r in rows]}

def status_by_month(input):
    rows = db.query("select substr(placed, 1, 7) as month, sum(case when status = 'paid' then 1 else 0 end) as paid, sum(case when status = 'pending' then 1 else 0 end) as pending, sum(case when status = 'refunded' then 1 else 0 end) as refunded from orders group by month order by month")
    return {"data": [{"month": months[r["month"][5:7]], "paid": r["paid"], "pending": r["pending"], "refunded": r["refunded"]} for r in rows]}

def orders_by_status(input):
    rows = db.query("select status, count(*) as n from orders group by status order by n desc")
    return {"data": [{"label": r["status"].capitalize(), "value": r["n"], "tone": status_tones[r["status"]]} for r in rows]}

def revenue_by_product(input):
    rows = db.query("select product, sum(amount) / 100 as revenue from orders where status = 'paid' group by product order by revenue desc limit ?", input["props"]["limit"])
    return {"data": rows}

def revenue_per_day(input):
    rows = db.query("select placed as date, sum(amount) / 100 as value from orders where status = 'paid' group by placed order by placed")
    return {"data": rows}

def commits(input):
    """A year of made-up daily commit counts ending Sat 2026-09-26: a fixed
    formula (no randomness, no clock), quieter at weekends, so the demo
    renders the same bytes every time."""
    days = db.query("with recursive d(i, day) as (select 0, '2026-09-26' union all select i + 1, date(day, '-1 day') from d where i < 370) select i, day from d")
    out = []
    for r in days:
        i = r["i"]
        dow = (6 - i) % 7
        weekend = dow == 0 or dow == 6
        n = (i * 7919 + (i * i) % 97) % 13
        v = 0 if n < (8 if weekend else 3) else n - (7 if weekend else 2)
        out.append({"date": r["day"], "value": v})
    return {"data": out}
