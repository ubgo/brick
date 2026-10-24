package statessql

import "testing"

// TestDialectsAreIndependent pins why the dialects are functions: replacing
// a field on one returned value must not change the next one.
func TestDialectsAreIndependent(t *testing.T) {
	for name, fn := range map[string]func() Dialect{"postgres": Postgres, "sqlite": SQLite, "mysql": MySQL} {
		d := fn()
		d.Name = "mutated"
		d.Upsert = func(string) string { return "mutated" }
		if got := fn(); got.Name != name || got.Upsert("t") == "mutated" {
			t.Errorf("%s: a mutated copy leaked into the next call: %+v", name, got.Name)
		}
	}
}
