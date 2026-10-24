// Re-render the page you are looking at, the way the dev server's live
// reload does: with navigation on, brick.refresh() fetches the page again
// and morphs it in place (scroll, focus and typed input survive); without
// it brick.refresh does not exist, so fall back to a full reload.
export function refreshNow() {
  if (window.brick && brick.refresh) return brick.refresh()
  location.reload()
}
