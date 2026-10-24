package scriptstarlark

import (
	"fmt"
	"maps"
	"slices"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/ubgo/brick"
)

// brickModule is the name of the predeclared effects module
// . One namespaced name, so an action's
// brick.flash(...) never collides with a module's own flash or title.
const brickModule = "brick"

// newBrickModule builds `brick`: thin wrappers over brick.Effects. Each
// finds the action's collector through the thread's context; outside the
// action function itself — a loader, a template function, the re-render
// after an action — there is none, and the call is a script error, so
// drawing a page can never fire an event.
//
//	brick.dispatch(name, data = None, to = None)
//	brick.redirect(url)            brick.navigate(url)
//	                               (redirect is in route.go: it also
//	                               answers from a loader)
//	brick.flash(message, level = "info")
//	brick.clear_flash(level = None) dismiss shown flashes, all or one
//	                               level (v21 J2)
//	brick.title(text)
//	brick.transition(types = "")   this answer's view transition: types
//	                               ("slide"), "" untyped, "none" to skip (v22)
//	brick.download(url, filename = None)
//	brick.url(url, replace = False)
//	brick.validate(fields = None)  → dict of path → message
//	brick.untouch(fields = None)   fields may name a wildcard pattern
//	                               (todos.*.title, v19)
//	brick.reply(value)             the value brick.action() resolves with
//	brick.skip_render()            save the state, answer without HTML
//	brick.stream(ref, text, replace = False)
//	                               send text to a brick:ref now
//	brick.rows(ref, op = "insert", component = None, items = [], key = "id",
//	           at = "bottom", limit = 0, keys = [])
//	                               edit a brick:ref list's rows
//	brick.region(ref, op = "replace")
//	                               apply a brick:ref brick:ignore region of
//	                               this render: replace, append, prepend (v22)
//	brick.submit(form)             natively submit a brick:ref form
//	brick.publish(topic)           refresh every component listening to
//	                               topic, once the action succeeded (v17)
//	brick.start_job(name, args = None, topic = None)
//	                               → the job's id; start a background job
//	                               once the action succeeded
//	brick.cancel_job(id)           cancel a job this component started,
//	                               once the action succeeded (v21 J1)
func newBrickModule() starlark.Value {
	members := starlark.StringDict{}
	add := func(name string, fn func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error) {
		full := brickModule + "." + name
		members[name] = starlark.NewBuiltin(full, func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			eff := brick.EffectsFrom(threadCtx(t))
			if eff == nil {
				return nil, fmt.Errorf("%s can only be called inside an action, never while a page renders", full)
			}
			if err := fn(eff, b, args, kwargs); err != nil {
				return nil, fmt.Errorf("%s: %w", full, err)
			}
			return starlark.None, nil
		})
	}
	add("dispatch", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var name string
		var data starlark.Value = starlark.None
		var to starlark.Value = starlark.None
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name, "data?", &data, "to?", &to); err != nil {
			return err
		}
		var m map[string]any
		if data != starlark.None {
			v, err := fromStarlark(data, 0)
			if err != nil {
				return fmt.Errorf("data: %w", err)
			}
			mm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("data must be a dict, not %s", data.Type())
			}
			m = mm
		}
		target := ""
		if to != starlark.None {
			s, ok := starlark.AsString(to)
			if !ok {
				return fmt.Errorf("to must be a component name or \"self\", not %s", to.Type())
			}
			target = s
		}
		return eff.Dispatch(name, m, target)
	})
	add("navigate", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var u string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &u); err != nil {
			return err
		}
		return eff.Navigate(u)
	})
	add("flash", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var msg string
		level := brick.FlashInfo
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "message", &msg, "level?", &level); err != nil {
			return err
		}
		return eff.Flash(msg, level)
	})
	add("clear_flash", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var level string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "level?", &level); err != nil {
			return err
		}
		return eff.ClearFlash(level)
	})
	add("transition", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var types string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "types?", &types); err != nil {
			return err
		}
		return eff.Transition(types)
	})
	add("cancel_job", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var id string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
			return err
		}
		return eff.CancelJob(id)
	})
	add("title", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var text string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "text", &text); err != nil {
			return err
		}
		return eff.Title(text)
	})
	add("publish", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var topic string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "topic", &topic); err != nil {
			return err
		}
		return eff.Publish(topic)
	})
	add("download", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var u, filename string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &u, "filename?", &filename); err != nil {
			return err
		}
		return eff.Download(u, filename)
	})
	add("url", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var u string
		var replace bool
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &u, "replace?", &replace); err != nil {
			return err
		}
		return eff.URL(u, replace)
	})
	add("region", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var ref, op string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "ref", &ref, "op?", &op); err != nil {
			return err
		}
		return eff.Region(ref, op)
	})
	add("submit", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var form string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "form", &form); err != nil {
			return err
		}
		return eff.Submit(form)
	})
	add("reply", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		var v starlark.Value
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "value", &v); err != nil {
			return err
		}
		gv, err := fromStarlark(v, 0)
		if err != nil {
			return fmt.Errorf("value: %w", err)
		}
		return eff.Reply(gv)
	})
	add("skip_render", func(eff *brick.Effects, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) error {
		if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
			return err
		}
		return eff.SkipRender()
	})
	// start_job returns the job's id, so it is not one of the add wrappers
	// (they return None); the name is checked against the host's Go
	// registry, never resolved to a script or a file.
	members["start_job"] = starlark.NewBuiltin(brickModule+".start_job", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		eff := brick.EffectsFrom(threadCtx(t))
		if eff == nil {
			return nil, fmt.Errorf("%s can only be called inside an action, never while a page renders", b.Name())
		}
		var name, topic string
		var data starlark.Value = starlark.None
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name, "args?", &data, "topic?", &topic); err != nil {
			return nil, err
		}
		var m map[string]any
		if data != starlark.None {
			v, err := fromStarlark(data, 0)
			if err != nil {
				return nil, fmt.Errorf("%s: args: %w", b.Name(), err)
			}
			mm, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: args must be a dict, not %s", b.Name(), data.Type())
			}
			m = mm
		}
		id, err := eff.StartJob(name, m, topic)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return starlark.String(id), nil
	})
	// stream needs the action's context (its ActionStream), not only the
	// effects collector, so it is not one of the add wrappers; it still
	// refuses outside an action.
	members["stream"] = starlark.NewBuiltin(brickModule+".stream", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		ctx := threadCtx(t)
		if brick.EffectsFrom(ctx) == nil {
			return nil, fmt.Errorf("%s can only be called inside an action, never while a page renders", b.Name())
		}
		var ref, text string
		var replace bool
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "ref", &ref, "text", &text, "replace?", &replace); err != nil {
			return nil, err
		}
		if err := brick.StreamFrom(ctx).Write(ref, text, replace); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return starlark.None, nil
	})
	// rows renders components, so it needs the action's context like
	// stream; there is deliberately no html argument (brick.Effects.Rows).
	members["rows"] = starlark.NewBuiltin(brickModule+".rows", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		ctx := threadCtx(t)
		eff := brick.EffectsFrom(ctx)
		if eff == nil {
			return nil, fmt.Errorf("%s can only be called inside an action, never while a page renders", b.Name())
		}
		var s brick.RowsSpec
		var items, keys starlark.Value = starlark.None, starlark.None
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "ref", &s.Ref, "op?", &s.Op, "component?", &s.Component, "items?", &items, "key?", &s.Key, "at?", &s.At, "limit?", &s.Limit, "keys?", &keys); err != nil {
			return nil, err
		}
		if items != starlark.None {
			v, err := fromStarlark(items, 0)
			if err != nil {
				return nil, fmt.Errorf("%s: items: %w", b.Name(), err)
			}
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: items must be a list of dicts, not %s", b.Name(), items.Type())
			}
			for i, x := range list {
				m, ok := x.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: items[%d] must be a dict", b.Name(), i)
				}
				s.Items = append(s.Items, m)
			}
		}
		if keys != starlark.None {
			v, err := fromStarlark(keys, 0)
			if err != nil {
				return nil, fmt.Errorf("%s: keys: %w", b.Name(), err)
			}
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: keys must be a list, not %s", b.Name(), keys.Type())
			}
			for _, k := range list {
				s.Keys = append(s.Keys, fmt.Sprint(k))
			}
		}
		if err := eff.Rows(ctx, s); err != nil {
			return nil, fmt.Errorf("%s: %w", b.Name(), err)
		}
		return starlark.None, nil
	})
	addValidation(members)
	addRoute(members)
	return starlarkstruct.FromStringDict(starlark.String(brickModule), members)
}

// addValidation adds brick.validate and brick.untouch:
// thin wrappers over brick.Validation, found through the thread's context
// like the effects collector. A component without brick: {rules:} has
// none, and the call is a script error naming why.
func addValidation(members starlark.StringDict) {
	wrap := func(name string, fn func(t *starlark.Thread, v *brick.Validation, fields []string) (starlark.Value, error)) {
		full := brickModule + "." + name
		members[name] = starlark.NewBuiltin(full, func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var raw starlark.Value = starlark.None
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "fields?", &raw); err != nil {
				return nil, err
			}
			v := brick.ValidationFrom(threadCtx(t))
			if v == nil {
				return nil, fmt.Errorf("%s can only be called inside an action of a component with brick: {rules:}", full)
			}
			fields, err := fieldList(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", full, err)
			}
			out, err := fn(t, v, fields)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", full, err)
			}
			return out, nil
		})
	}
	wrap("validate", func(t *starlark.Thread, v *brick.Validation, fields []string) (starlark.Value, error) {
		errs, err := v.Validate(threadCtx(t), fields...)
		if err != nil {
			return nil, err
		}
		d := starlark.NewDict(len(errs))
		for _, k := range slices.Sorted(maps.Keys(errs)) {
			if err := d.SetKey(starlark.String(k), starlark.String(errs[k])); err != nil {
				return nil, err
			}
		}
		return d, nil
	})
	wrap("untouch", func(_ *starlark.Thread, v *brick.Validation, fields []string) (starlark.Value, error) {
		v.Untouch(fields...)
		return starlark.None, nil
	})
}

// fieldList reads fields = None, a path, or a list of paths.
func fieldList(v starlark.Value) ([]string, error) {
	if v == starlark.None {
		return nil, nil
	}
	if s, ok := starlark.AsString(v); ok {
		return []string{s}, nil
	}
	it, ok := v.(starlark.Iterable)
	if !ok {
		return nil, fmt.Errorf("fields must be a path or a list of paths, not %s", v.Type())
	}
	var out []string
	iter := it.Iterate()
	defer iter.Done()
	var x starlark.Value
	for iter.Next(&x) {
		s, ok := starlark.AsString(x)
		if !ok {
			return nil, fmt.Errorf("fields must be paths (strings), not %s", x.Type())
		}
		out = append(out, s)
	}
	return out, nil
}
