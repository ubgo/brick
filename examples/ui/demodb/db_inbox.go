package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// inboxSchema is the Inbox app's table (content/apps/inbox.html, apps/inbox/inbox.star).
// Its closed sets are documented beside the columns because the Starlark
// actions and the pages both write them.
const inboxSchema = `
create table messages (
  id integer primary key,
  sender text not null,
  email text not null,
  subject text not null,
  body text not null,
  folder text not null,   -- inbox | archive | trash | sent (written by the Inbox compose action)
  unread integer not null,
  starred integer not null,
  sent text not null      -- ISO date
);
`

// subjects are the seeded message subjects, cycled by row.
var subjects = []string{"Quarterly numbers are in", "Can we move Thursday's call?", "Design review notes", "Your invoice #1042", "Welcome to the beta", "Re: pricing feedback", "Offsite venue options", "Security update required", "New comment on your post", "Lunch next week?"}

// seedInbox creates and fills the Inbox app's tables. seed() in db.go calls it.
func seedInbox(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, inboxSchema); err != nil {
		return fmt.Errorf("inbox schema: %w", err)
	}
	const messagesN = 18
	for i := range messagesN {
		f := firstNames[(i*5)%len(firstNames)]
		args := []any{f + " " + lastNames[i%len(lastNames)], lower(f) + "@example.com", subjects[i%len(subjects)], "Hi,\n\nHere are the details we talked about. Let me know what you think when you get a chance.\n\nThanks,\n" + f, []string{folderInbox, folderInbox, folderInbox, folderArchive}[i%4], boolInt(i%3 == 0), boolInt(i%5 == 0), day(260 - i)}
		if _, err := db.ExecContext(ctx, "insert into messages (sender, email, subject, body, folder, unread, starred, sent) values (?, ?, ?, ?, ?, ?, ?, ?)", args...); err != nil {
			return fmt.Errorf("inbox seed: %w", err)
		}
	}
	return nil
}
