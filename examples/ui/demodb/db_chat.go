package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// chatSchema is the Chat app's tables (content/apps/chat.html,
// apps/chat/chat.star). They live beside the app rather than in schema so the two
// can change without touching each other; seed runs this after schema.
const chatSchema = `
create table channels (
  id integer primary key,
  slug text not null unique,  -- the URL-safe key ChatApp's state holds
  name text not null,         -- "general" for a channel, the person for a DM
  kind text not null,         -- channel | dm
  topic text not null,
  presence text not null,     -- online | away | offline for a DM, '' for a channel
  last_read integer not null  -- the highest chat_messages.id the viewer (chatMe) has seen here
);
create table channel_members (
  channel_id integer not null, -- channels.id
  name text not null,
  role text not null           -- owner | member | bot
);
create table chat_messages (
  id integer primary key,      -- also the order of the conversation
  channel_id integer not null, -- channels.id
  sender text not null,        -- a member's name; chatMe for the viewer
  body text not null,
  day text not null,           -- ISO date
  time text not null,          -- HH:MM, 24-hour
  attachment text not null,    -- a file name, or ''
  pending integer not null     -- 1 while a bot reply is "typing": hidden until the next render delivers it (apps/chat/chat.star deliver)
);
`

// chatMe is the signed-in person; apps/chat/chat.star ME must match.
const chatMe = "Ada Lovelace"

// chatDays are the seeded conversation days, oldest first.
var chatDays = []string{"2026-09-25", "2026-09-26", "2026-09-27"}

// ChatLastDay is the last seeded conversation day. The chat app continues
// #general's clock from its last message today, so a test that checks the
// stamps of new messages needs to know whether today is that day.
func ChatLastDay() string { return chatDays[len(chatDays)-1] }

// chatChannels are the seeded channels and DMs, in sidebar order. unread is
// how many of the channel's newest messages from others start unread.
var chatChannels = []struct {
	slug, name, kind, topic, presence string
	members                           []string
	unread                            int
}{
	{"general", "general", chatChannel, "Company-wide news and announcements", "", []string{chatMe, "Maya Chen", "Liam Kim", "Noah Silva", "Zara Singh", "Omar Haddad"}, 0},
	{"design", "design", chatChannel, "Critiques, Figma links and the design system", "", []string{chatMe, "Maya Chen", "Zara Singh"}, 2},
	{"engineering", "engineering", chatChannel, "Deploys, incidents and code review", "", []string{chatMe, "Liam Kim", "Noah Silva", "Omar Haddad"}, 3},
	{"launch-v2", "launch-v2", chatChannel, "Everything for the v2 launch on Oct 1", "", []string{chatMe, "Maya Chen", "Liam Kim", "Zara Singh"}, 0},
	{"maya", "Maya Chen", chatDM, "Product design lead", "online", []string{chatMe, "Maya Chen"}, 1},
	{"liam", "Liam Kim", chatDM, "Staff engineer", "away", []string{chatMe, "Liam Kim"}, 0},
	{"brickbot", "Brickbot", chatDM, "A demo bot: it answers every message you send", "online", []string{chatMe, "Brickbot"}, 0},
	{"noah", "Noah Silva", chatDM, "Platform engineer", "offline", []string{chatMe, "Noah Silva"}, 0},
}

// chatSeed is the seeded conversation: channel slug, index into chatDays,
// time, sender, body and attachment. Rows are inserted in this order, so
// ids follow it; keep each channel's rows in time order.
var chatSeed = []struct {
	slug         string
	day          int
	time, sender string
	body, attach string
}{
	{"general", 0, "09:02", "Maya Chen", "Morning all! The v2 launch plan is pinned in #launch-v2.", ""},
	{"general", 0, "09:05", "Liam Kim", "Nice. Staging is green as of last night.", ""},
	{"general", 1, "10:14", "Noah Silva", "Reminder: the office is closed on Monday for the offsite.", ""},
	{"general", 1, "10:16", chatMe, "Thanks Noah — I'll share the agenda by Friday.", ""},
	{"general", 2, "08:47", "Zara Singh", "Coffee machine on 3 is fixed ☕", ""},
	{"general", 2, "08:48", "Omar Haddad", "Best news of the week.", ""},

	{"design", 1, "14:30", "Maya Chen", "New pricing page explorations are up.", "pricing-v3.fig"},
	{"design", 1, "14:31", "Maya Chen", "Option B tested best with the five users we spoke to.", ""},
	{"design", 1, "15:02", chatMe, "B reads much clearer. Can we try the toggle above the cards?", ""},
	{"design", 2, "09:20", "Zara Singh", "Dark mode tokens for the charts are done.", "chart-tokens.png"},
	{"design", 2, "09:21", "Zara Singh", "Contrast passes AA everywhere except the zinc accent — looking into it.", ""},

	{"engineering", 0, "16:40", "Liam Kim", "Deploy 812 is out. Median render dropped to 38 ms.", ""},
	{"engineering", 0, "16:42", chatMe, "Great work. Anything to watch?", ""},
	{"engineering", 0, "16:45", "Liam Kim", "Only the cache warm-up on cold starts. Tracking it in the incident doc.", "incident-812.md"},
	{"engineering", 2, "11:02", "Noah Silva", "Heads up: rotating the database credentials at noon.", ""},
	{"engineering", 2, "11:05", "Omar Haddad", "Runbook updated.", ""},
	{"engineering", 2, "11:30", "Noah Silva", "Done — no downtime.", ""},

	{"launch-v2", 1, "09:00", "Maya Chen", "Launch checklist: copy ✅ screenshots ✅ pricing ⏳", ""},
	{"launch-v2", 1, "09:12", "Liam Kim", "Feature flags are ready to flip on the 1st.", ""},
	{"launch-v2", 2, "13:10", chatMe, "Press embargo lifts at 9:00 PT. Please don't post before then.", ""},

	{"maya", 1, "18:05", chatMe, "Do you have a minute tomorrow to go over the hero copy?", ""},
	{"maya", 2, "08:31", "Maya Chen", "Sure! How about 10:30?", ""},
	{"maya", 2, "08:32", "Maya Chen", "I'll bring the two headline options.", ""},

	{"liam", 0, "12:15", "Liam Kim", "Lunch Thursday?", ""},
	{"liam", 0, "12:20", chatMe, "Yes — the ramen place?", ""},
	{"liam", 0, "12:21", "Liam Kim", "Perfect 🍜", ""},

	{"brickbot", 2, "09:00", "Brickbot", "Hi! I'm Brickbot. Send me a message and I'll answer on the next refresh — the page polls every 5 seconds.", ""},

	{"noah", 1, "17:45", "Noah Silva", "The new staging box is yours whenever you want it.", ""},
}

// seedChat creates the chat tables and fills them with chatSeed. last_read
// is set so each channel's newest `unread` messages from others count as
// unread.
func seedChat(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, chatSchema); err != nil {
		return fmt.Errorf("chat schema: %w", err)
	}
	ids := map[string]int64{}
	for _, c := range chatChannels {
		res, err := db.ExecContext(ctx, "insert into channels (slug, name, kind, topic, presence, last_read) values (?, ?, ?, ?, ?, 0)", c.slug, c.name, c.kind, c.topic, c.presence)
		if err != nil {
			return fmt.Errorf("seed channels: %w", err)
		}
		if ids[c.slug], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("seed channels: %w", err)
		}
		for i, m := range c.members {
			role := roleMember
			switch {
			case m == "Brickbot":
				role = roleBot
			case i == 1 && c.kind == chatChannel:
				role = roleOwner
			}
			if _, err := db.ExecContext(ctx, "insert into channel_members (channel_id, name, role) values (?, ?, ?)", ids[c.slug], m, role); err != nil {
				return fmt.Errorf("seed members: %w", err)
			}
		}
	}
	for _, m := range chatSeed {
		if _, err := db.ExecContext(ctx, "insert into chat_messages (channel_id, sender, body, day, time, attachment, pending) values (?, ?, ?, ?, ?, ?, 0)", ids[m.slug], m.sender, m.body, chatDays[m.day], m.time, m.attach); err != nil {
			return fmt.Errorf("seed chat: %w", err)
		}
	}
	for _, c := range chatChannels {
		// Everything is read except the newest c.unread messages from others.
		if _, err := db.ExecContext(ctx, `update channels set last_read = coalesce((
			select id from chat_messages where channel_id = ?1 and sender != ?2 order by id desc limit 1 offset ?3
		), (select max(id) from chat_messages where channel_id = ?1), 0) where id = ?1`, ids[c.slug], chatMe, c.unread); err != nil {
			return fmt.Errorf("seed last_read: %w", err)
		}
	}
	return nil
}
