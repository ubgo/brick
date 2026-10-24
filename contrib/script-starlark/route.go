package scriptstarlark

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/ubgo/brick"
)

// addRoute adds a loader's answers: what a loader says
// instead of data. Each but header fails the call with the root's typed
// answer (brick.ErrNotFound, *brick.Redirect, *brick.HTTPError), which
// Module.Call keeps unwrappable, so the site — or a Go host's own router —
// turns it into the response. Unlike the effects they work anywhere — a
// loader is exactly where a missing record or a signed-out visitor is
// found.
//
//	brick.not_found()
//	brick.redirect(url, status = 303)   302, 303, 307 or 308
//	brick.error(status, message = "")   a 4xx or 5xx
//	brick.header(name, value)           one of brick.LoaderHeaders()
//
// brick.redirect inside an action function stays the action effect (a
// client navigation); status= is a loader's answer only
// and is refused there.
func addRoute(members starlark.StringDict) {
	members["not_found"] = starlark.NewBuiltin(brickModule+".not_found", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
			return nil, err
		}
		return nil, brick.ErrNotFound
	})
	members["redirect"] = starlark.NewBuiltin(brickModule+".redirect", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var u string
		status := 0
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &u, "status?", &status); err != nil {
			return nil, err
		}
		if eff := brick.EffectsFrom(threadCtx(t)); eff != nil {
			if status != 0 {
				return nil, fmt.Errorf("%s: status= is for a loader's redirect; an action's redirect is a client navigation", b.Name())
			}
			if err := eff.Redirect(u); err != nil {
				return nil, fmt.Errorf("%s: %w", b.Name(), err)
			}
			return starlark.None, nil
		}
		rd, err := brick.NewRedirect(u, status)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return nil, rd
	})
	members["error"] = starlark.NewBuiltin(brickModule+".error", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var status int
		var msg string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "status", &status, "message?", &msg); err != nil {
			return nil, err
		}
		he, err := brick.NewHTTPError(status, msg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return nil, he
	})
	members["header"] = starlark.NewBuiltin(brickModule+".header", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name, value string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name, "value", &value); err != nil {
			return nil, err
		}
		if err := brick.SetHeader(threadCtx(t), name, value); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return starlark.None, nil
	})
}
