package brick

import (
	"math"
	"strings"
	"testing"
)

// The arithmetic built-ins on every pairing of kinds: two integers stay
// integral (div truncates), any float makes the result a float, and a zero
// divisor is an error for div and for integer mod, never a panic or an Inf.
func TestArithMatrix(t *testing.T) {
	t.Parallel()
	type tc struct {
		op   arithOp
		a, b any
		want any
	}
	for _, c := range []tc{
		{opAdd, 7, 2, int64(9)}, {opSub, 7, 2, int64(5)}, {opMul, 7, 2, int64(14)},
		{opDiv, 7, 2, int64(3)}, {opMod, 7, 2, int64(1)}, {opMax, 7, 2, int64(7)}, {opMin, 7, 2, int64(2)},
		{opDiv, -7, 2, int64(-3)}, {opMod, -7, 2, int64(-1)},
		{opAdd, int64(1), int32(2), int64(3)},
		{opAdd, 7, 2.5, 9.5}, {opSub, 7.5, 2, 5.5}, {opMul, 2.5, 2, 5.0},
		{opDiv, 7.0, 2, 3.5}, {opMod, 7.5, 2, 1.5}, {opMax, 7, 2.5, 7.0}, {opMin, 7, 2.5, 2.5},
		{opMax, -0.5, -1, -0.5}, {opMin, -0.5, -1, -1.0},
		{opAdd, "1.5", 1, 2.5},
	} {
		got, err := arith(c.op)(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("%s(%v, %v) = %v (%T), %v; want %v (%T)", c.op, c.a, c.b, got, got, err, c.want, c.want)
		}
	}
	for _, c := range []tc{{op: opDiv, a: 1, b: 0}, {op: opMod, a: 1, b: 0}, {op: opDiv, a: 1.5, b: 0}} {
		if got, err := arith(c.op)(c.a, c.b); err == nil || !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("%s(%v, %v) = %v, %v; want a division-by-zero error", c.op, c.a, c.b, got, err)
		}
	}
	// Float mod by zero follows math.Mod: NaN, not an error.
	if got, err := arith(opMod)(1.5, 0); err != nil || !math.IsNaN(got.(float64)) {
		t.Errorf("mod(1.5, 0) = %v, %v; want NaN", got, err)
	}
	if _, err := arith(opAdd)("x", 1); err == nil || !strings.HasPrefix(err.Error(), "add: ") {
		t.Errorf("add(\"x\", 1) error = %v; want one naming the op", err)
	}
	// An op with no case is an error, not a silent min (the old fall-through).
	if got, err := arith(arithOp("pow"))(2.0, 3.0); err == nil {
		t.Errorf("an unknown op computed %v", got)
	}
}

// Every arithOp is registered under its own name, so a template's {{ add }}
// reaches arith(opAdd) and not another op.
func TestArithOpsRegistered(t *testing.T) {
	t.Parallel()
	funcs := pureBuiltins()
	for _, op := range []arithOp{opAdd, opSub, opMul, opDiv, opMod, opMax, opMin} {
		f, ok := funcs[string(op)].(func(a, b any) (any, error))
		if !ok {
			t.Fatalf("%s is not registered as an arithmetic built-in", op)
		}
		want, _ := arith(op)(9, 4)
		if got, _ := f(9, 4); got != want {
			t.Errorf("{{ %s 9 4 }} = %v, want %v", op, got, want)
		}
	}
}

// describe quotes at most describeMax characters of a non-string value, so
// a record passed where a string was expected keeps the message readable.
func TestDescribeTruncates(t *testing.T) {
	t.Parallel()
	long := make([]int, 100)
	d := describe(long)
	head, _, ok := strings.Cut(d, " (")
	if !ok || len(head) != describeMax || !strings.HasSuffix(head, describeEllipsis) {
		t.Errorf("describe of a long slice = %q; want %d characters ending in %q, then the type", d, describeMax, describeEllipsis)
	}
	if d := describe([]int{1}); d != "[1] ([]int)" {
		t.Errorf("describe of a short slice = %q", d)
	}
}
