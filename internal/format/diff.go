package format

import (
	"bytes"
	"fmt"
)

// diffContext is how many unchanged lines a unified diff shows around a
// change, the same as diff -u and gofmt -d.
const diffContext = 3

// op is one line of an edit script.
type op struct {
	kind byte // ' ', '-' or '+'
	text []byte
}

// Diff returns a unified diff from old to new under the name, or nil when
// they are equal. It needs no external diff program, so `brick fmt -d`
// works the same on every machine and in CI.
func Diff(name string, old, new []byte) []byte {
	if bytes.Equal(old, new) {
		return nil
	}
	a, b := splitLines(old), splitLines(new)
	ops := script(a, b)
	var out bytes.Buffer
	fmt.Fprintf(&out, "--- %s\n+++ %s (formatted)\n", name, name)
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// A hunk: back up for context, then run until diffContext*2
		// unchanged lines separate it from the next change.
		start := max(0, i-diffContext)
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*diffContext {
				end = min(end+diffContext, len(ops))
				break
			}
			end = run
		}
		aStart, bStart := lineNumbers(ops[:start])
		aLen, bLen := 0, 0
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				aLen++
			}
			if o.kind != '-' {
				bLen++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart+1, aLen, bStart+1, bLen)
		for _, o := range ops[start:end] {
			out.WriteByte(o.kind)
			out.Write(o.text)
			if len(o.text) == 0 || o.text[len(o.text)-1] != '\n' {
				out.WriteString("\n\\ No newline at end of file\n")
			}
		}
		i = end
	}
	return out.Bytes()
}

// lineNumbers counts the old and new lines the ops consume.
func lineNumbers(ops []op) (a, b int) {
	for _, o := range ops {
		if o.kind != '+' {
			a++
		}
		if o.kind != '-' {
			b++
		}
	}
	return a, b
}

func splitLines(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	return bytes.SplitAfter(b, []byte("\n"))[:bytes.Count(b, []byte("\n"))+boolInt(b[len(b)-1] != '\n')]
}

// script is a shortest edit script by longest common subsequence, after
// trimming the common head and tail (a formatted file differs from its
// source in a few places, so the table stays small).
func script(a, b [][]byte) []op {
	var head, tail []op
	for len(a) > 0 && len(b) > 0 && bytes.Equal(a[0], b[0]) {
		head = append(head, op{' ', a[0]})
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && bytes.Equal(a[len(a)-1], b[len(b)-1]) {
		tail = append([]op{{' ', a[len(a)-1]}}, tail...)
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if bytes.Equal(a[i], b[j]) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	ops := head
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && bytes.Equal(a[i], b[j]):
			ops = append(ops, op{' ', a[i]})
			i, j = i+1, j+1
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	return append(ops, tail...)
}
