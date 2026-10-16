package boox

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/noteactions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	ocrapi "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// Note actions: handwriting in an assigned pen color becomes tasks or
// calendar events (internal/alexandria/noteactions). They ride on automatic
// recognition: when a page is recognized, its colored ink is noted, and a page
// whose colored ink changed is queued here. Only ink written after note
// actions started counts, so turning them on never sweeps up old notes.

// ActionsConfig is what the page pipeline needs from the owner's settings.
type ActionsConfig struct {
	// Enabled means recognition, a summary model and automatic recognition
	// are all set up and note actions aren't turned off.
	Enabled bool
	// Red and Blue are "task", "event" or "off".
	Red, Blue                           string
	TaskInstructions, EventInstructions string
	// PublicURL makes item links absolute ("" = relative links).
	PublicURL string
}

func (a ActionsConfig) action(color string) string {
	if color == "blue" {
		return a.Blue
	}
	return a.Red
}

// inkColors are the pen colors note actions read.
var inkColors = []struct {
	Name string
	ARGB int32
}{{"red", booxpage.InkRed}, {"blue", booxpage.InkBlue}}

// pageInk summarizes one color's ink on a page.
type pageInk struct {
	Color  string
	Count  int
	Hash   string
	Newest time.Time
}

func inkSummary(p *booxpage.Page) []pageInk {
	var out []pageInk
	for _, c := range inkColors {
		shapes := booxpage.InkShapes(p, c.ARGB)
		if len(shapes) == 0 {
			continue
		}
		var newest int64
		for _, s := range shapes {
			newest = max(newest, s.CreatedAt, s.UpdatedAt)
		}
		in := pageInk{Color: c.Name, Count: len(shapes), Hash: booxpage.InkHash(shapes)}
		if newest > 0 {
			in.Newest = time.UnixMilli(newest)
		}
		out = append(out, in)
	}
	return out
}

// actionsSince is when note actions started: ink older than this is never
// acted on automatically. It is set once, the first time it is needed.
func (s Service) actionsSince(ctx context.Context) (time.Time, error) {
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_settings(key,value,secret) VALUES($1,to_jsonb(to_char(now() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')),false) ON CONFLICT (key) DO NOTHING`, settings.NoteActionsSince); e != nil {
		return time.Time{}, e
	}
	v, e := settings.Get(ctx, s.DB, settings.NoteActionsSince, "")
	if e != nil {
		return time.Time{}, e
	}
	return time.Parse(time.RFC3339, v)
}

// noteInk queues note actions for a page's colored ink when it is new or
// changed. force (the page's Find actions button) ignores when it was written.
func (s Service) noteInk(ctx context.Context, id, page string, ink []pageInk, force bool) (int, error) {
	if !s.Actions.Enabled || len(ink) == 0 {
		return 0, nil
	}
	since, e := s.actionsSince(ctx)
	if e != nil {
		return 0, e
	}
	queued := 0
	for _, in := range ink {
		action := s.Actions.action(in.Color)
		if action != "task" && action != "event" {
			continue
		}
		// Ink without timestamps can't be dated, so only the button acts on it.
		if !force && (in.Newest.IsZero() || in.Newest.Before(since)) {
			continue
		}
		res, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_action(source,notebook_id,page_id,color,action,ink_hash,page_saved_at)
 VALUES('boox',$1,$2,$3,$4,$5,$6)
 ON CONFLICT(source,notebook_id,page_id,color) DO UPDATE SET action=EXCLUDED.action,ink_hash=EXCLUDED.ink_hash,page_saved_at=EXCLUDED.page_saved_at,
  state='queued',attempts=0,next_at=now(),lease_token='',lease_until=NULL,detail='',requested_at=now(),updated_at=now()
 WHERE alexandria_note_action.ink_hash<>EXCLUDED.ink_hash OR alexandria_note_action.action<>EXCLUDED.action OR ($7 AND alexandria_note_action.state<>'processing')`,
			id, page, in.Color, action, in.Hash, nullTime(in.Newest), force)
		if e != nil {
			return queued, e
		}
		if n, _ := res.RowsAffected(); n > 0 {
			queued++
		}
	}
	return queued, nil
}

func nullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: !t.IsZero()} }

// FindActions queues note actions for one page now (the notebook view's
// button), whenever its ink was written. It reports how many colors were queued.
func (s Service) FindActions(ctx context.Context, id string, num int) (int, error) {
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return 0, e
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	if n.Meta.Status != 1 || num < 1 || num > len(ids) {
		return 0, errors.New("page unavailable")
	}
	p, _, e := booxpage.ShapePage(n.Meta, ids[num-1], n.Keys, s.snapshotLoader(ctx, n))
	if e != nil {
		return 0, e
	}
	return s.noteInk(ctx, id, ids[num-1], inkSummary(p), true)
}

// ProcessNoteActions performs one leased note action: recognize the colored
// ink alone, ask the summary model for items, and create the ones this page
// hasn't produced before. Items are never changed afterwards.
func (s Service) ProcessNoteActions(ctx context.Context, ocr notes.OCR, summary noteactions.Completer) (bool, error) {
	var id, page, color, action, oldInput, oldText string
	var attempts int
	var saved sql.NullTime
	token := uuid.NewString()
	e := s.DB.QueryRowContext(ctx, `UPDATE alexandria_note_action a SET state='processing',lease_token=$1,lease_until=now()+interval '10 minutes',updated_at=now()
 WHERE (source,notebook_id,page_id,color)=(SELECT source,notebook_id,page_id,color FROM alexandria_note_action WHERE source='boox' AND state IN ('queued','processing') AND next_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING notebook_id,page_id,color,action,attempts,input_hash,ocr_text,page_saved_at`, token).Scan(&id, &page, &color, &action, &attempts, &oldInput, &oldText, &saved)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	const where = ` WHERE source='boox' AND notebook_id=$1 AND page_id=$2 AND color=$3 AND lease_token=$4`
	finish := func(state, detail, input, text, model string) (bool, error) {
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_action SET state=$5,detail=$6,input_hash=$7,ocr_text=$8,model=$9,attempts=0,finished_at=now(),lease_token='',lease_until=NULL,updated_at=now()`+where,
			id, page, color, token, state, limitDetail(detail), input, text, model)
		return true, e
	}
	retry := func(detail string, terminal bool) (bool, error) {
		if terminal || attempts >= 4 {
			if !terminal {
				detail = ocrapi.GaveUp(detail)
			}
			return finish("failed", detail, oldInput, oldText, "")
		}
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_action SET state='queued',detail=$5,attempts=attempts+1,next_at=now()+$6::interval,lease_token='',lease_until=NULL,updated_at=now()`+where,
			id, page, color, token, limitDetail(detail), backoff(attempts))
		return true, e
	}
	if action != "task" && action != "event" || s.Actions.action(color) != action {
		return finish("ready", "This pen color no longer has an action; nothing was done.", oldInput, oldText, "")
	}
	if ocr == nil || summary == nil {
		return retry("Note actions need a recognition provider and a summary model.", false)
	}
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return finish("ready", "Notebook unavailable.", oldInput, oldText, "")
	}
	if e != nil {
		return retry("Could not read page snapshot; retrying.", false)
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	num := 0
	for i, v := range ids {
		if v == page {
			num = i + 1
		}
	}
	if n.Meta.Status != 1 || num == 0 {
		return finish("ready", "Page was removed.", oldInput, oldText, "")
	}
	p, _, e := booxpage.ShapePage(n.Meta, page, n.Keys, s.snapshotLoader(ctx, n))
	if e != nil {
		return retry("Page files aren't all readable yet: "+limitDetail(e.Error()), false)
	}
	argb := booxpage.InkRed
	if color == "blue" {
		argb = booxpage.InkBlue
	}
	img := booxpage.InkImage(p, booxpage.InkShapes(p, argb))
	if img == nil {
		return finish("ready", "No ink in this color on the page.", oldInput, "", "")
	}
	jpegData, _, e := ocrapi.EncodePage(img)
	if e != nil {
		return retry("Recognition image unavailable.", false)
	}
	sum := sha256.Sum256([]byte(noteactions.PromptVersion + "\x00" + ocr.Model() + "\x00" + noteactions.OCRPrompt + "\x00" + string(jpegData)))
	input := hex.EncodeToString(sum[:])
	text := oldText
	if input != oldInput {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		text, e = ocr.Recognize(rctx, jpegData, noteactions.OCRPrompt)
		cancel()
		if e != nil {
			again, detail := ocrapi.Failure(e)
			return retry(detail, !again)
		}
		text = strings.TrimSpace(text)
		// Keep the transcript even if the next step fails, so a retry doesn't pay for it twice.
		if _, e = s.DB.ExecContext(ctx, `UPDATE alexandria_note_action SET input_hash=$5,ocr_text=$6,updated_at=now()`+where, id, page, color, token, input, text); e != nil {
			return true, e
		}
		oldInput, oldText = input, text
	}
	if text == "" {
		return finish("ready", "The colored ink had no readable text.", input, text, ocr.Model())
	}
	zone := settings.Zone(ctx, s.DB)
	written := time.Now()
	if saved.Valid {
		written = saved.Time
	}
	instructions := s.Actions.TaskInstructions
	if action == "event" {
		instructions = s.Actions.EventInstructions
	}
	items, e := noteactions.Extract(ctx, summary, action, instructions, noteactions.Input{Notebook: n.Meta.Title, Page: num, Written: written, Zone: zone, Text: text})
	if errors.Is(e, noteactions.ErrFormat) {
		return retry("The summary model's answer didn't follow the note action format.", false)
	}
	if e != nil {
		again, detail := ocrapi.Failure(e)
		return retry(detail, !again)
	}
	made, skipped, dropped, e := s.makeItems(ctx, id, page, color, action, token, n.Meta.Title, num, zone, items)
	if e != nil {
		return true, e
	}
	return finish("ready", actionDetail(action, made, skipped, dropped), input, text, ocr.Model())
}

func actionDetail(action string, made, skipped, dropped int) string {
	kind := "task"
	if action == "event" {
		kind = "event"
	}
	plural := func(n int, s string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", s)
		}
		return fmt.Sprintf("%d %ss", n, s)
	}
	d := "Made " + plural(made, kind) + "."
	if skipped > 0 {
		d += fmt.Sprintf(" %d already made from this page.", skipped)
	}
	if dropped > 0 {
		d += fmt.Sprintf(" %d without a recognizable date skipped.", dropped)
	}
	return d
}

// itemLink is the page an item came from, absolute when the server knows its address.
func (s Service) itemLink(id, page string) string {
	return strings.TrimRight(s.Actions.PublicURL, "/") + "/boox/notebook?id=" + url.QueryEscape(id) + "&page_id=" + url.QueryEscape(page)
}

// makeItems creates the items whose passages this page hasn't produced yet.
// The ledger row is claimed first, under the lease, so a passage is acted on
// once even if two runs overlap.
func (s Service) makeItems(ctx context.Context, id, page, color, action, token, notebook string, num int, zone *time.Location, items []noteactions.Item) (made, skipped, dropped int, err error) {
	link := s.itemLink(id, page)
	from := fmt.Sprintf("From %s, page %d.", strings.TrimSpace(notebook), num)
	if strings.TrimSpace(notebook) == "" {
		from = fmt.Sprintf("From a BOOX notebook, page %d.", num)
	}
	for _, it := range items {
		var e events.Event
		var t taskstore.Task
		var itemID string
		if action == "event" {
			var ok bool
			if e, ok = eventFromItem(it, zone); !ok {
				dropped++
				continue
			}
			e.ID, e.URL, e.Detail = events.NewID(), link, strings.TrimSpace(it.Detail+"\n\n"+from)
			itemID = e.ID
		} else {
			now := time.Now().UnixMilli()
			t = taskstore.Task{TaskID: taskstore.GenerateTaskID(it.Title+"\x00"+id+"\x00"+page, now), Title: taskstore.SqlStr(it.Title),
				Status: taskstore.SqlStr("needsAction"), IsDeleted: "N", Links: taskstore.SqlStr(link), Detail: taskstore.SqlStr(strings.TrimSpace(it.Detail + "\n\n" + from))}
			if it.Due != "" {
				if d, perr := time.ParseInLocation("2006-01-02", it.Due, zone); perr == nil {
					t.DueTime = d.UnixMilli()
				}
			}
			itemID = t.TaskID
		}
		fp := noteactions.Fingerprint(it.Source)
		if fp == "" {
			fp = noteactions.Fingerprint(it.Title)
		}
		res, qerr := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_action_item(source,notebook_id,page_id,color,fingerprint,kind,item_id,source_text)
 SELECT 'boox',$1,$2,$3,$4,$5,$6,$7 WHERE EXISTS(SELECT 1 FROM alexandria_note_action WHERE source='boox' AND notebook_id=$1 AND page_id=$2 AND color=$3 AND lease_token=$8)
 ON CONFLICT DO NOTHING`, id, page, color, fp, action, itemID, it.Source, token)
		if qerr != nil {
			return made, skipped, dropped, qerr
		}
		if n, _ := res.RowsAffected(); n == 0 {
			skipped++
			continue
		}
		var cerr error
		if action == "event" {
			cerr = events.NewStore(s.DB).Create(ctx, &e)
		} else {
			cerr = taskdb.NewStore(s.DB).Create(ctx, &t)
		}
		if cerr != nil {
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM alexandria_note_action_item WHERE source='boox' AND notebook_id=$1 AND page_id=$2 AND color=$3 AND fingerprint=$4`, id, page, color, fp)
			return made, skipped, dropped, cerr
		}
		made++
	}
	return made, skipped, dropped, nil
}

// eventFromItem builds an event from a model item; false when its date
// isn't usable. A timed event lasts an hour unless an end time is given.
func eventFromItem(it noteactions.Item, zone *time.Location) (events.Event, bool) {
	date, err := time.ParseInLocation("2006-01-02", it.Date, zone)
	if err != nil {
		return events.Event{}, false
	}
	last := date
	if it.EndDate != "" {
		if d, err := time.ParseInLocation("2006-01-02", it.EndDate, zone); err == nil && !d.Before(date) {
			last = d
		}
	}
	e := events.Event{Title: it.Title, Location: it.Location}
	clock := func(day time.Time, v string) (time.Time, bool) {
		c, err := time.Parse("15:04", v)
		if err != nil {
			return time.Time{}, false
		}
		return time.Date(day.Year(), day.Month(), day.Day(), c.Hour(), c.Minute(), 0, 0, zone), true
	}
	start, ok := clock(date, it.Time)
	if !ok {
		e.AllDay = true
		e.Start = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
		e.End = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		return e, true
	}
	e.Start, e.End, e.TZID = start.UTC(), start.Add(time.Hour).UTC(), settings.ZoneName(zone)
	if end, ok := clock(last, it.EndTime); ok && end.After(start) {
		e.End = end.UTC()
	} else if !last.Equal(date) {
		if end, ok := clock(last, it.Time); ok {
			e.End = end.UTC()
		}
	}
	return e, true
}

// pageAction is one color's note action state on a page.
type pageAction struct {
	Color, Action, State, Detail, Text string
}

// pageActionItem is something a page's ink made.
type pageActionItem struct {
	Kind, ID, Title, Source string
	Gone                    bool
}

// URL is the item's edit page.
func (i pageActionItem) URL() string {
	if i.Kind == "event" {
		return "/calendar/" + url.PathEscape(i.ID) + "/edit"
	}
	return "/tasks/" + url.PathEscape(i.ID) + "/edit"
}

func (s Service) pageActions(ctx context.Context, id, page string) ([]pageAction, []pageActionItem, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT color,action,state,detail,ocr_text FROM alexandria_note_action WHERE source='boox' AND notebook_id=$1 AND page_id=$2 ORDER BY color DESC`, id, page)
	if e != nil {
		return nil, nil, e
	}
	var acts []pageAction
	for rows.Next() {
		var a pageAction
		if e = rows.Scan(&a.Color, &a.Action, &a.State, &a.Detail, &a.Text); e != nil {
			rows.Close()
			return nil, nil, e
		}
		acts = append(acts, a)
	}
	rows.Close()
	rows, e = s.DB.QueryContext(ctx, `SELECT i.kind,i.item_id,coalesce(t.title,e.title,''),i.source_text,
 coalesce(t.deleted,e.deleted,true)
 FROM alexandria_note_action_item i
 LEFT JOIN alexandria_tasks t ON i.kind='task' AND t.task_id=i.item_id
 LEFT JOIN alexandria_events e ON i.kind='event' AND e.event_id=i.item_id
 WHERE i.source='boox' AND i.notebook_id=$1 AND i.page_id=$2 ORDER BY i.created_at`, id, page)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	var items []pageActionItem
	for rows.Next() {
		var it pageActionItem
		if e = rows.Scan(&it.Kind, &it.ID, &it.Title, &it.Source, &it.Gone); e != nil {
			return nil, nil, e
		}
		items = append(items, it)
	}
	return acts, items, rows.Err()
}

func (s Service) findActionsHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	num, _ := strconv.Atoi(r.Form.Get("page"))
	back := "/boox/notebook?id=" + url.QueryEscape(id) + "&page=" + strconv.Itoa(num)
	if !s.Actions.Enabled {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape(i18n.M("Note actions need automatic recognition, a recognition provider and a summary model.")), http.StatusSeeOther)
		return
	}
	n, e := s.FindActions(r.Context(), id, num)
	if e != nil {
		http.Error(w, "Page unavailable", 422)
		return
	}
	notice := i18n.M("No ink in a note-action color on this page.")
	if n > 0 {
		notice = i18n.M("Looking for actions on this page.")
	}
	http.Redirect(w, r, back+"&notice="+url.QueryEscape(notice), http.StatusSeeOther)
}
