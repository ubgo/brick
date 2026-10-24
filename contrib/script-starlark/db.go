package scriptstarlark

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// newDB builds the `db` capability (D8):
//
//	db.query(sql, *args)          read-only, SQL fixed at compile time
//	db.query_dynamic(sql, *args)  the named escape for SQL built at run time
//	db.exec(sql, *args)           writes; only with Options.DBExec
func newDB(e *Engine) starlark.Value {
	members := starlark.StringDict{
		"query":         starlark.NewBuiltin("db.query", e.dbQuery),
		"query_dynamic": starlark.NewBuiltin("db.query_dynamic", e.dbQuery),
	}
	if e.opts.DBExec {
		members["exec"] = starlark.NewBuiltin("db.exec", e.dbExec)
	}
	return starlarkstruct.FromStringDict(starlark.String("db"), members)
}

// errMultiStatement rejects several statements in one call (D8).
var errMultiStatement = errors.New("one statement per call: the SQL contains ;")

// sqlArgs splits a builtin's arguments into SQL and values.
func sqlArgs(b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (string, []any, error) {
	if len(kwargs) > 0 {
		return "", nil, fmt.Errorf("%s: takes no keyword arguments", b.Name())
	}
	if len(args) == 0 {
		return "", nil, fmt.Errorf("%s: missing the SQL", b.Name())
	}
	q, ok := starlark.AsString(args[0])
	if !ok {
		return "", nil, fmt.Errorf("%s: the SQL must be a string", b.Name())
	}
	q = strings.TrimRight(strings.TrimSpace(q), ";")
	if strings.Contains(q, ";") {
		return "", nil, fmt.Errorf("%s: %w", b.Name(), errMultiStatement)
	}
	vals := make([]any, len(args)-1)
	for i, a := range args[1:] {
		v, err := fromStarlark(a, 0)
		if err != nil {
			return "", nil, fmt.Errorf("%s: argument %d: %w", b.Name(), i+2, err)
		}
		vals[i] = v
	}
	return q, vals, nil
}

// dbQuery runs a query inside a read-only transaction that is rolled back,
// and returns a list of dicts.
func (e *Engine) dbQuery(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	q, vals, err := sqlArgs(b, args, kwargs)
	if err != nil {
		return nil, err
	}
	ctx := threadCtx(t)
	tx, err := e.opts.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, q, vals...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	defer rows.Close()
	cols, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	seen := map[string]bool{}
	for i, c := range cols {
		names[i] = c.Name()
		if seen[c.Name()] {
			return nil, fmt.Errorf("%s: two columns are named %q; rename one with AS", b.Name(), c.Name())
		}
		seen[c.Name()] = true
	}
	var out []starlark.Value
	for rows.Next() {
		if len(out) >= e.opts.DBMaxRows {
			return nil, fmt.Errorf("%s: more than %d rows; add a LIMIT (D8)", b.Name(), e.opts.DBMaxRows)
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := starlark.NewDict(len(cols))
		for i, v := range raw {
			sv, err := columnValue(cols[i], v)
			if err != nil {
				return nil, fmt.Errorf("%s: column %q: %w", b.Name(), names[i], err)
			}
			if err := row.SetKey(starlark.String(names[i]), sv); err != nil {
				return nil, err
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return starlark.NewList(out), nil
}

// columnValue converts one database value (D8): NULL → None, decimals →
// strings (no float rounding), times → ISO strings, bytes → bytes.
func columnValue(c *sql.ColumnType, v any) (starlark.Value, error) {
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case []byte:
		t := strings.ToUpper(c.DatabaseTypeName())
		if strings.Contains(t, "DECIMAL") || strings.Contains(t, "NUMERIC") || strings.Contains(t, "CHAR") || strings.Contains(t, "TEXT") {
			return starlark.String(string(x)), nil
		}
		return starlark.Bytes(string(x)), nil
	case time.Time:
		return starlark.String(x.Format(time.RFC3339Nano)), nil
	}
	return toStarlark(v, 0, nil)
}

// dbExec runs a write and returns the number of rows affected.
func (e *Engine) dbExec(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	q, vals, err := sqlArgs(b, args, kwargs)
	if err != nil {
		return nil, err
	}
	res, err := e.opts.DB.ExecContext(threadCtx(t), q, vals...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	return starlark.MakeInt64(n), nil
}
