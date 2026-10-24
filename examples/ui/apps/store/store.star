# The Store demo app: the catalog query, the cart and checkout, over the
# products, cart and orders tables in demodb/db_store.go. StoreApp calls the queries from
# its template and these functions as its actions (brick:click="store.add(3)").
#
# Every action returns the new state and sets cart_open: True for the ones
# fired from inside the cart drawer, so the re-rendered drawer stays open,
# and False for the rest, so a drawer the shopper closed stays closed.

CATEGORIES = ["Bags", "Clothing", "Home", "Stationery"]

SORTS = ["featured", "price_asc", "price_desc", "rating", "name"]

# One literal query per sort order: the SQL is always written out, never
# built from strings (brick's rule for db.query), so the ORDER BY is chosen
# by picking a query, not by pasting a column name.
Q_FEATURED = "select id, name, price, category, rating, stock from products where (? = '' or lower(name) like ?) and (? = 0 or price <= ?) and (? = 0 or stock > 0) and (? = '' or instr(?, ',' || category || ',') > 0) order by id"
Q_PRICE_ASC = "select id, name, price, category, rating, stock from products where (? = '' or lower(name) like ?) and (? = 0 or price <= ?) and (? = 0 or stock > 0) and (? = '' or instr(?, ',' || category || ',') > 0) order by price, id"
Q_PRICE_DESC = "select id, name, price, category, rating, stock from products where (? = '' or lower(name) like ?) and (? = 0 or price <= ?) and (? = 0 or stock > 0) and (? = '' or instr(?, ',' || category || ',') > 0) order by price desc, id"
Q_RATING = "select id, name, price, category, rating, stock from products where (? = '' or lower(name) like ?) and (? = 0 or price <= ?) and (? = 0 or stock > 0) and (? = '' or instr(?, ',' || category || ',') > 0) order by rating desc, id"
Q_NAME = "select id, name, price, category, rating, stock from products where (? = '' or lower(name) like ?) and (? = 0 or price <= ?) and (? = 0 or stock > 0) and (? = '' or instr(?, ',' || category || ',') > 0) order by name, id"

# ICONS draws each product's placeholder picture; TONES tints it by category.
ICONS = {"Linen shirt": "shirt", "Canvas tote": "shopping-bag", "Ceramic mug": "coffee", "Wool beanie": "sparkles", "Desk lamp": "lamp", "Leather wallet": "wallet", "Notebook set": "notebook", "Fountain pen": "pen-tool"}
TONES = {"Bags": "warning", "Clothing": "primary", "Home": "success", "Stationery": "info"}

# LOW_STOCK is the count at or under which a product shows "Only N left".
LOW_STOCK = 5

# SHIPPING_FREE_OVER is the subtotal, in cents, above which shipping is free;
# SHIPPING is the flat rate under it.
SHIPPING_FREE_OVER = 10000
SHIPPING = 600

def money(cents):
    c = cents % 100
    return "$%d.%s%d" % (cents // 100, "0" if c < 10 else "", c)

def _decorate(p):
    stock = p["stock"]
    return dict(
        p,
        price_label = money(p["price"]),
        icon = ICONS.get(p["name"], "package"),
        tone = TONES.get(p["category"], "neutral"),
        stars = int(p["rating"] + 0.5),
        rating_label = str(int(p["rating"])) + "." + str(int(p["rating"] * 10 + 0.5) % 10),
        stock_state = "out" if stock == 0 else ("low" if stock <= LOW_STOCK else "in"),
    )

def products(state):
    q = state["q"].strip().lower()
    like = "%" + q + "%"
    cats = "," + ",".join(state["cats"]) + "," if state["cats"] else ""
    cap = state["max_price"] * 100
    args = (q, like, cap, cap, 1 if state["in_stock"] else 0, cats, cats)
    sort = state["sort"]
    if sort == "price_asc":
        rows = db.query(Q_PRICE_ASC, *args)
    elif sort == "price_desc":
        rows = db.query(Q_PRICE_DESC, *args)
    elif sort == "rating":
        rows = db.query(Q_RATING, *args)
    elif sort == "name":
        rows = db.query(Q_NAME, *args)
    else:
        rows = db.query(Q_FEATURED, *args)
    return [_decorate(p) for p in rows]

def facets(state):
    rows = db.query("select category, count(*) as n from products group by category order by category")
    counts = {r["category"]: r["n"] for r in rows}
    top = db.query("select max(price) as cents from products")[0]["cents"]
    return {
        "categories": [{"i": i, "name": c, "n": counts.get(c, 0), "on": c in state["cats"]} for i, c in enumerate(CATEGORIES)],
        "ceiling": (top + 99) // 100,
        "total": db.query("select count(*) as n from products")[0]["n"],
    }

def cart(state):
    lines = db.query("select p.id, p.name, p.category, p.price, p.stock, c.qty from cart c join products p on p.id = c.product_id order by p.name")
    out = []
    count = 0
    subtotal = 0
    for l in lines:
        total = l["price"] * l["qty"]
        count += l["qty"]
        subtotal += total
        out.append(dict(l, price_label = money(l["price"]), total_label = money(total), icon = ICONS.get(l["name"], "package"), tone = TONES.get(l["category"], "neutral")))
    shipping = 0 if subtotal == 0 or subtotal >= SHIPPING_FREE_OVER else SHIPPING
    return {
        "lines": out,
        "count": count,
        "subtotal": money(subtotal),
        "shipping": "Free" if shipping == 0 else money(shipping),
        "total": money(subtotal + shipping),
        "free_left": money(SHIPPING_FREE_OVER - subtotal) if subtotal > 0 and subtotal < SHIPPING_FREE_OVER else "",
        "free_pct": min(100, subtotal * 100 // SHIPPING_FREE_OVER),
    }

# ---- actions ------------------------------------------------------------

# toggle_cat flips category i. From a checkbox's brick:change it also gets
# the box's new checked value, which it follows; from a tag's remove button
# it gets nothing and turns the category off.
def toggle_cat(state, props, i, on = False):
    c = CATEGORIES[i]
    cats = sorted([x for x in state["cats"] if x != c] + ([c] if on else []))
    return dict(state, cats = cats, cart_open = False)

def set_sort(state, props, sort):
    return dict(state, sort = sort if sort in SORTS else "featured", cart_open = False)

def clear_filters(state, props):
    return dict(state, q = "", cats = [], in_stock = False, max_price = 0, sort = "featured", cart_open = False)

def add(state, props, id):
    stock = db.query("select stock from products where id = ?", id)
    if not stock or stock[0]["stock"] == 0:
        return dict(state, cart_open = False)
    have = db.query("select qty from cart where product_id = ?", id)
    if have:
        db.exec("update cart set qty = min(qty + 1, ?) where product_id = ?", stock[0]["stock"], id)
    else:
        db.exec("insert into cart (product_id, qty) values (?, 1)", id)
    return dict(state, cart_open = False)

def set_qty(state, props, id, qty):
    n = int(qty) if type(qty) != "string" or qty.isdigit() else 1
    if n < 1:
        db.exec("delete from cart where product_id = ?", id)
    else:
        db.exec("update cart set qty = min(?, (select stock from products where id = ?)) where product_id = ?", n, id, id)
    return dict(state, cart_open = True)

def remove(state, props, id):
    db.exec("delete from cart where product_id = ?", id)
    return dict(state, cart_open = True)

def checkout(state, props):
    if not db.query("select product_id from cart limit 1"):
        return dict(state, cart_open = True)
    return dict(state, view = "checkout", cart_open = False)

def back(state, props):
    return dict(state, view = "shop", cart_open = False)

def place_order(state, props, fields):
    f = {k: fields.get(k, "").strip() for k in ["name", "email", "address", "city", "zip", "country"]}
    new = dict(state, form = f, cart_open = False)
    errors = {}
    if len(f["name"]) < 2:
        errors["name"] = "Enter the name for the delivery."
    at = f["email"].find("@")
    if at < 1 or "." not in f["email"][at:]:
        errors["email"] = "Enter an e-mail address like ada@example.com."
    if len(f["address"]) < 5:
        errors["address"] = "Enter a street and number."
    if len(f["city"]) < 2:
        errors["city"] = "Enter a city."
    if len(f["zip"]) < 3 or len(f["zip"]) > 10:
        errors["zip"] = "Enter a postal code."
    if not f["country"]:
        errors["country"] = "Choose a country."
    lines = db.query("select p.name, p.price, c.qty from cart c join products p on p.id = c.product_id order by p.name")
    if not lines:
        errors["cart"] = "Your cart is empty."
    if errors:
        return [new, errors]
    total = 0
    items = 0
    for l in lines:
        db.exec("insert into orders (customer, product, amount, status, placed) values (?, ?, ?, 'paid', ?)", f["name"], l["name"], l["price"] * l["qty"], today())
        total += l["price"] * l["qty"]
        items += l["qty"]
    db.exec("delete from cart")
    number = db.query("select max(id) as id from orders")[0]["id"]
    shipping = 0 if total >= SHIPPING_FREE_OVER else SHIPPING
    order = {"number": "#%d" % (1000 + number), "items": items, "total": money(total + shipping), "email": f["email"], "city": f["city"]}
    return dict(new, view = "done", order = order)

def keep_shopping(state, props):
    return dict(state, view = "shop", order = {}, form = {}, cart_open = False)
