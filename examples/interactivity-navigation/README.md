# interactivity-navigation

A three-page site with `navigation: true` through the site layer: `brick:current`, `brick:prefetch`, `brick:scroll`, `brick:persist` and `brick:nav="off"` in one layout, and fragment navigation, where a navigation to a page with the same layout gets only the page. The test shows the full page, the fragment and the fallback for a stale fingerprint.

The guide that walks through it: [Navigation without reloads](../../docs/guides/interactivity/navigation.md).

## Run it

```sh
cd examples/interactivity-navigation
go run .
```

Then open http://localhost:8094. `-addr` changes the listen address and `-theme` the project directory.

## Test it

```sh
cd examples
go test -v ./interactivity-navigation/
```

```text
--- PASS: TestFragmentNavigation (0.00s)
ok  	github.com/ubgo/brick/examples/interactivity-navigation	0.026s
```

`TestFragmentNavigation` makes a first visit (the whole page with `nav.js`, the layout fingerprint and the speculation rules), then a navigation that shows the same layout and gets only the page. Following links without a reload, keeping a persisted element and restoring a scroll position happen in the browser, so the test checks the markup and headers the server sends for them.
