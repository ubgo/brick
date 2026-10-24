def list():
    """One feed per tag: /tags/go/feed.xml and /tags/web/feed.xml."""
    return [{"tag": "go"}, {"tag": "web"}]
