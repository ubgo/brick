# The posts a real site would read from a database or a CMS.
POSTS = {
    "hello": {"title": "Hello world", "body": "The first post."},
    "tides": {"title": "Reading tide tables", "body": "Two highs a day."},
}

def all():
    """Every URL of content/posts/[slug].html: what brick build writes."""
    return [{"slug": slug} for slug in sorted(POSTS)]

def one(input):
    """The post the URL names; a slug not in POSTS is the 404 page."""
    slug = input["page"]["route"]["slug"]
    if slug not in POSTS:
        brick.not_found()
    return POSTS[slug]
