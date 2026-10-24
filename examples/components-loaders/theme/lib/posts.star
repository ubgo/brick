load("~/lib/text.star", "shout")
load("~/data/posts.yaml", posts = "data")

MOVED = {"old-hello": "/posts/hello"}
REMOVED = ["gone"]

def recent(input):
    return {"items": posts[:input["props"]["limit"]]}

def one(input):
    slug = input["page"]["route"]["slug"]
    if slug in MOVED:
        brick.redirect(MOVED[slug], status = 308)
    if slug in REMOVED:
        brick.error(410, "This post was removed.")
    for p in posts:
        if p["slug"] == slug:
            brick.header("Cache-Control", "public, max-age=300")
            return {"title": shout(p["title"]), "by": greeting}
    brick.not_found()
