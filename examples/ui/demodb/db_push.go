package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// pushSchema is the server push demo's table (content/interactivity.html,
// demos/RuntimePushPost.html and demos/RuntimePushBoard.html): notes any
// visitor posts, which every open board shows live.
const pushSchema = `
create table push_notes (
  id integer primary key,
  body text not null
);
`

// pushNotes are the board's first notes, so it renders the same bytes on
// every run until someone posts.
var pushNotes = []string{"Welcome to the live board.", "Open this page in a second window and post a note."}

// seedPush creates and fills the push demo's table. seed() in db.go calls it.
func seedPush(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, pushSchema); err != nil {
		return fmt.Errorf("push schema: %w", err)
	}
	for _, b := range pushNotes {
		if _, err := db.ExecContext(ctx, "insert into push_notes (body) values (?)", b); err != nil {
			return fmt.Errorf("push seed: %w", err)
		}
	}
	return nil
}
