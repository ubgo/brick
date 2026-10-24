package demodb

import (
	"context"
	"database/sql"
	"fmt"
)

// tasksSchema is the Task board app's table (content/apps/tasks.html, apps/tasks/tasks.star).
// Its closed sets are documented beside the columns because the Starlark
// actions and the pages both write them.
const tasksSchema = `
create table tasks (
  id integer primary key,
  title text not null,
  status text not null,   -- todo | doing | done
  priority text not null, -- low | medium | high
  assignee text not null,
  due text not null,      -- ISO date
  position integer not null
);
`

// taskTitles are the seeded tasks, one row each, in board order.
var taskTitles = []string{"Write the launch post", "Fix the flaky login test", "Design the pricing page", "Migrate billing to v2", "Review the Q3 roadmap", "Add dark mode to emails", "Interview two designers", "Ship the mobile nav", "Audit accessibility", "Set up error alerts", "Update the onboarding tour", "Plan the team offsite"}

// seedTasks creates and fills the Task board app's tables. seed() in db.go calls it.
func seedTasks(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, tasksSchema); err != nil {
		return fmt.Errorf("task board schema: %w", err)
	}
	for i := range len(taskTitles) {
		args := []any{taskTitles[i], []string{taskTodo, taskDoing, taskDone}[i%3], []string{priorityHigh, priorityMedium, priorityLow, priorityMedium}[i%4], firstNames[i%len(firstNames)], day(250 + i*3), i}
		if _, err := db.ExecContext(ctx, "insert into tasks (title, status, priority, assignee, due, position) values (?, ?, ?, ?, ?, ?)", args...); err != nil {
			return fmt.Errorf("tasks seed: %w", err)
		}
	}
	return nil
}
