package brick

import (
	"fmt"
	"strconv"
	"strings"
)

// Form objects.
// A stateful component's template reads a field's value from .State, its
// error from .Errors and whether the user has visited it from .Touched,
// three lookups by the same path that a form repeats for every input.
// .Form joins them into one value per field:
//
//	{{ with .Form.email }}
//	  <label for="{{ .ID }}">E-mail</label>
//	  <input id="{{ .ID }}" brick:model.lazy="{{ .Name }}" value="{{ .Value }}">
//	  {{ if .Error }}<p>{{ .Error }}</p>{{ end }}
//	{{ end }}
//
// It is a view over the scope's own State, Errors and Touched, computed
// when read, so it can never disagree with them and nothing new is stored
// in an instance. List fields (the v19 todos.*.title rules) are reached
// with .Items, whose paths carry the index: todos.2.title, the same
// concrete path the validator and brick:model use.

// Form is a stateful component's fields by top-level state key. A key the
// state does not declare behaves exactly as .State.x does: an error under
// strictParams (Options.StrictParams or the file's own), an empty value
// otherwise.
type Form map[string]FormField

// FormField is one field of a Form: everything a form control needs about
// the value at one state path.
type FormField struct {
	// Name is the dotted state path (address.city, todos.2.title): what
	// brick:model and brick:submit field names take.
	Name string
	// ID is the component instance's id joined with the path, for a
	// label's for= and aria-describedby: unique per instance on the page
	// and the same on every render (E5).
	ID string
	// Value is the state's value at Name; nil when the path does not
	// resolve (an index past the end of a list).
	Value any
	// Error is the message .Errors holds for Name, "" when none: a rule
	// error once the field is touched, or the error the last action
	// returned, which wins.
	Error string
	// Touched reports whether the user has sent this path or an action
	// validated it.
	Touched bool

	src *formSource
}

// formSource is what every field of one Form reads its error and touched
// state from: the scope's maps, shared, never copied.
type formSource struct {
	id      string
	errors  map[string]any
	touched map[string]bool
}

// formIDSep joins the instance id and the path in FormField.ID; path dots
// become it too, since a dot in an id needs escaping in a CSS selector.
const formIDSep = "-"

// Form returns the component's fields (see Form). Outside a stateful
// component it is empty.
func (s *Scope) Form() Form {
	src := &formSource{id: s.ID, errors: s.Errors, touched: s.Touched}
	out := make(Form, len(s.State))
	for k, v := range s.State {
		out[k] = src.field(k, v)
	}
	return out
}

// field builds the field at path holding value.
func (src *formSource) field(path string, value any) FormField {
	f := FormField{Name: path, Value: value, Touched: src.touched[path], src: src}
	if src.id != "" {
		f.ID = src.id + formIDSep + strings.ReplaceAll(path, ".", formIDSep)
	} else {
		f.ID = strings.ReplaceAll(path, ".", formIDSep)
	}
	if e, ok := src.errors[path]; ok && e != nil {
		if s, isStr := e.(string); isStr {
			f.Error = s
		} else {
			f.Error = fmt.Sprint(e)
		}
	}
	return f
}

// Fields returns the fields of a dict value by key (address.Fields.city
// is the field address.city). A value that is not a dict has none, so
// ranging over it renders nothing rather than failing the page.
func (f FormField) Fields() Form {
	d, ok := f.Value.(map[string]any)
	if !ok || f.src == nil {
		return Form{}
	}
	out := make(Form, len(d))
	for k, v := range d {
		out[k] = f.src.field(f.Name+"."+k, v)
	}
	return out
}

// Items returns one field per element of a list value, in order, each
// named by its index (todos.0, todos.1): .Fields on an item reaches its
// keys (todos.1.title), and a list of plain values uses the item's own
// Value. A value that is not a list has none.
func (f FormField) Items() []FormField {
	l, ok := f.Value.([]any)
	if !ok || f.src == nil {
		return nil
	}
	out := make([]FormField, len(l))
	for i, v := range l {
		out[i] = f.src.field(f.Name+"."+strconv.Itoa(i), v)
	}
	return out
}

// Invalid reports whether the field has an error to show: the check a
// template writes for aria-invalid and an error style.
func (f FormField) Invalid() bool { return f.Error != "" }
