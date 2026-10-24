package brick

import (
	"fmt"
	"log/slog"
	"strings"
)

// ActionLog is a component's `brick: {log:}` setting: how loud an action logger should be about this
// component's actions. The engine only carries it, on ActionCall.Log; a
// logging middleware (actionmw.Slog, a host's own) decides what it means.
// nil on ActionCall means the component did not say, so the logger uses
// its own default.
type ActionLog struct {
	// Off is `log: false`: the component's successful actions are not
	// logged. actionmw.Slog still logs a failed or refused action, because
	// a refusal is a security signal one file must not be able to hide.
	Off bool
	// Level is the level a successful action logs at (`log: debug`).
	Level slog.Level
}

// The words `brick: {log:}` accepts besides true and false, mapped to the
// slog levels they name. A closed set, so a typo is a compile error
// instead of a component that silently logs at the wrong level.
const (
	actionLogDebug = "debug"
	actionLogInfo  = "info"
	actionLogWarn  = "warn"
	actionLogError = "error"
)

// actionLogLevels maps each word to its level.
var actionLogLevels = map[string]slog.Level{
	actionLogDebug: slog.LevelDebug,
	actionLogInfo:  slog.LevelInfo,
	actionLogWarn:  slog.LevelWarn,
	actionLogError: slog.LevelError,
}

// actionLogWords lists the level words in severity order, for the error
// message and the frontmatter schema.
var actionLogWords = []string{actionLogDebug, actionLogInfo, actionLogWarn, actionLogError}

// parseActionLog reads `brick: {log:}`. true is the same as leaving the
// key out (nil: the logger's default), false switches success logging
// off, and a level word sets the level.
func parseActionLog(v any) (*ActionLog, error) {
	switch x := v.(type) {
	case bool:
		if x {
			return nil, nil
		}
		return &ActionLog{Off: true}, nil
	case string:
		if lv, ok := actionLogLevels[x]; ok {
			return &ActionLog{Level: lv}, nil
		}
	}
	return nil, fmt.Errorf("brick: log: must be true, false or a level (%s), got %v", strings.Join(actionLogWords, ", "), v)
}
