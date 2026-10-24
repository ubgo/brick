# Loaders for the Data display demos: rows from the showcase's SQLite
# database (db.go seeds it with the same rows on every run, so the pages
# that show them render the same bytes every time).

def contacts(input):
    rows = db.query("select id, name, email, company, role, status, created from contacts order by id")
    return {"rows": rows}

def contacts_page(input):
    per = input["props"]["perPage"]
    page = input["props"]["page"]
    rows = db.query("select id, name, email, company, role, status from contacts order by id limit ? offset ?", per, (page - 1) * per)
    total = db.query("select count(*) as n from contacts")[0]["n"]
    return {"rows": rows, "total": total}

def recent_contacts(input):
    rows = db.query("select id, name, email, company, role, status from contacts order by created desc limit ?", input["props"]["limit"])
    return {"rows": rows}

def order_stats(input):
    paid = db.query("select count(*) as n, sum(amount) as cents from orders where status = 'paid'")[0]
    pending = db.query("select count(*) as n from orders where status = 'pending'")[0]
    refunded = db.query("select count(*) as n from orders where status = 'refunded'")[0]
    return {"orders": paid["n"], "revenue": paid["cents"] // 100, "pending": pending["n"], "refunded": refunded["n"]}
