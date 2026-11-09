package mcptools

// Every change an assistant makes is recorded, so the owner can see what was
// done and by which app, and the tools that cost money or add to the tablet
// are capped per day. A planted instruction in text an assistant read can't
// then do much, or anything unseen.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

// Caps on what one library's assistants may do per day, and per call.
const (
	recognizePagesCap = 50 // pages one recognize call may queue
	dailyNotesCap     = 10 // create_boox_note per day
	dailyMinutesCap   = 10 // request_minutes per day
)

// dailyCaps are the tools limited per day, by name.
var dailyCaps = map[string]int{"create_boox_note": dailyNotesCap, "request_minutes": dailyMinutesCap}

// Change is one recorded change.
type Change struct {
	At                              time.Time
	Client, Tool, Arguments, Result string
}

// record notes a change. Arguments and the reply's first line are kept,
// shortened; a failure to record is reported with the result.
func record(ctx context.Context, db pg.Querier, client, tool string, in any, result string) error {
	args, _ := json.Marshal(in)
	if first, _, _ := strings.Cut(result, "\n"); first != "" {
		result = first
	}
	_, err := db.ExecContext(ctx, `WITH gone AS (DELETE FROM alexandria_mcp_change WHERE at < now()-interval '90 days')
 INSERT INTO alexandria_mcp_change(client,tool,arguments,result) VALUES($1,$2,$3,$4)`, short(client, 100), tool, short(string(args), 2000), short(result, 500))
	return err
}

// allowed refuses a capped tool used up for the day.
func allowed(ctx context.Context, db pg.Querier, tool string) error {
	limit, ok := dailyCaps[tool]
	if !ok {
		return nil
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_mcp_change WHERE tool=$1 AND at > now()-interval '1 day'`, tool).Scan(&n); err != nil {
		return err
	}
	if n >= limit {
		return fmt.Errorf("%s is limited to %d a day over MCP, and that's used up; the owner can do more from the web app", tool, limit)
	}
	return nil
}

// RecentChanges lists the latest changes assistants made, newest first.
func RecentChanges(ctx context.Context, db pg.Querier, limit int) ([]Change, error) {
	rows, err := db.QueryContext(ctx, `SELECT at,client,tool,arguments,result FROM alexandria_mcp_change ORDER BY at DESC,id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.At, &c.Client, &c.Tool, &c.Arguments, &c.Result); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func short(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
