# site-protected

Pages that depend on who is asking. `/admin/` holds an island whose loader sends a signed-out visitor to `/login/` (303) and refuses a non-admin with 403, rendered by `content/error.html`; the page around the island stays cached. `/dashboard/` is a whole page rendered per visitor (`brick: {per_request: true}`). The visitor comes from a `user` cookie, standing in for your program's real session.

```sh
go run .                                         # http://localhost:8092
curl -i localhost:8092/admin/                    # 303 to /login/
curl -i --cookie user=bob localhost:8092/admin/  # 403 Admins only.
curl -i --cookie user=admin localhost:8092/admin/
go test .                                        # the responses, the cache isolation and the export
```

Guides: [protected and per-request pages](../../docs/guides/site/protected-pages.md), [404 and error pages](../../docs/guides/site/not-found-and-error-pages.md).
