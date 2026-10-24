package demodb

// Rule: NO BARE STRINGS for a value with a closed set of choices. The seed
// code (the db_<app>.go seeds) writes these values, and the apps'
// Starlark (apps/*/*.star) and pages read and write the same ones.
// Go cannot share constants with Starlark, so each set names the
// Starlark that must agree with it; the showcase tests (apps_*_test.go)
// fail when the two drift, because a seeded row an app does not recognize
// renders wrong.

// Contact statuses (contacts.status; apps/contacts/contacts.star STATUSES).
const (
	contactActive  = "active"
	contactLead    = "lead"
	contactChurned = "churned"
)

// Task statuses and priorities (tasks.status, tasks.priority; the Task
// board's columns and badges).
const (
	taskTodo  = "todo"
	taskDoing = "doing"
	taskDone  = "done"

	priorityHigh   = "high"
	priorityMedium = "medium"
	priorityLow    = "low"
)

// Message folders (messages.folder; the Inbox app's sidebar).
const (
	folderInbox   = "inbox"
	folderArchive = "archive"
)

// Order statuses (orders.status; the Dashboard's charts and table).
const (
	orderPaid     = "paid"
	orderPending  = "pending"
	orderRefunded = "refunded"
)

// Invoice statuses (invoices.status; apps/invoices/invoices.star). invoiceOverdue is
// never chosen by the seed pattern: a sent invoice past its due date is
// stored as overdue, the rule the app applies itself.
const (
	invoiceDraft   = "draft"
	invoiceSent    = "sent"
	invoicePaid    = "paid"
	invoiceOverdue = "overdue"
)

// Chat channel kinds and member roles (channels.kind, channel_members.role;
// apps/chat/chat.star).
const (
	chatChannel = "channel"
	chatDM      = "dm"

	roleMember = "member"
	roleOwner  = "owner"
	roleBot    = "bot"
)

// Post statuses (posts.status; apps/cms/cms.star STATUSES).
const (
	postDraft     = "draft"
	postScheduled = "scheduled"
	postPublished = "published"
)

// Tones: the kit's colour names, used by the Calendar's events and the
// CMS's post covers (apps/cms/cms.star COVERS). Each is a --bk-<tone> token.
const (
	tonePrimary = "primary"
	toneSuccess = "success"
	toneWarning = "warning"
	toneDanger  = "danger"
	toneInfo    = "info"
	toneNeutral = "neutral"
)
