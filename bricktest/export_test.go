package bricktest

import (
	"fmt"
	"strings"
)

// KeyEventForTest renders keyEvent as runtime.js writes KEY_EVENT, in the
// grammar's key order.
func KeyEventForTest() string {
	parts := make([]string, 0, len(grammarTable().Keys))
	for _, k := range grammarTable().Keys {
		if keyEvent[k] != "" { // a letter or digit is not in the table
			parts = append(parts, fmt.Sprintf("%s: %q", k, keyEvent[k]))
		}
	}
	return "const KEY_EVENT = { " + strings.Join(parts, ", ") + " };"
}
