# The batch loader of RuntimeUserCard (brick: {load_many: users.cards}):
# called once with every distinct card input on the page, it answers with a
# list in the same order. A real one would run one query with an IN list;
# batch reports how many inputs the single call carried, so each card shows
# that it came from that one call.
USERS = {
    "ada": {"name": "Ada Lovelace", "role": "Analyst"},
    "grace": {"name": "Grace Hopper", "role": "Compiler lead"},
    "linus": {"name": "Linus Torvalds", "role": "Kernel"},
    "margaret": {"name": "Margaret Hamilton", "role": "Flight software"},
}

def cards(inputs):
    return [dict(USERS[i["props"]["id"]], batch = len(inputs)) for i in inputs]
