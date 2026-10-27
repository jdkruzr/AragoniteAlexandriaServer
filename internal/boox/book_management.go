package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

// Server-side reorganization of the native reading library copies what the
// Library app writes (docs/reading-file-operations.md): soft upserts of status,
// name, parentUniqueId or libraryUniqueId with a fresh updatedAt. Only stored
// books (a live, non-empty book binding) and writable shelves are in scope.
// Devices keep one live membership per book (MergeMetadataCollectionHandler
// ensureDocumentCollectionUnique, newest updatedAt wins), so no change here
// ever gives a book a second shelf.
var (
	errLibraryChanged  = errors.New("the library changed; nothing was queued")
	errLibraryName     = errors.New("enter a name of 1–240 bytes without slashes or control characters")
	errLibraryConflict = errors.New("an item with this name already exists here")
	errLibraryShelves  = errors.New("BOOX keeps each book on one shelf; move it instead")
	errLibraryCycle    = errors.New("a shelf cannot move inside itself")
	errLibraryTooLarge = errors.New("this change touches too many library records")
)

const libraryTrash = ".Trash"
const libraryBatchLimit = 2000

type libraryShelf struct {
	ID, Parent, Name, Revision, Head string
	Live, Native                     bool // Native: not a remote, smart or storage shelf
}
type libraryBook struct {
	ID, Name, Title, Kind, Revision, Head, SHA, ContentType string
	Size, Modified                                          int64
	Live                                                    bool
}
type libraryMember struct {
	ID, Book, Shelf, Revision, Head string
	Live                            bool
}

// librarySnapshot is the projection plus every journaled library change it
// does not reflect yet, so consecutive edits see their own pending writes.
type librarySnapshot struct {
	Shelves map[string]*libraryShelf
	Books   map[string]*libraryBook // stored books only
	Members map[string]*libraryMember
}

func (s Service) librarySnapshot(ctx context.Context, db pg.Querier) (*librarySnapshot, error) {
	v := &librarySnapshot{Shelves: map[string]*libraryShelf{}, Books: map[string]*libraryBook{}, Members: map[string]*libraryMember{}}
	type binding struct {
		sha, ct     string
		size, stamp int64
	}
	stored := map[string]binding{}
	rows, e := db.QueryContext(ctx, `SELECT DISTINCT ON (split_part(l.native_key,'/',3)) split_part(l.native_key,'/',3),v.sha256,v.bytes,coalesce(v.content_type,''),(extract(epoch from l.published_at)*1000)::bigint FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE starts_with(l.native_key,$1||'/reader/') AND split_part(l.native_key,'/',4)='book' AND v.bytes>0 AND v.sha256 IS NOT NULL AND NOT v.deleted ORDER BY split_part(l.native_key,'/',3),l.native_key`, s.uid())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id string
		var b binding
		if e = rows.Scan(&id, &b.sha, &b.size, &b.ct, &b.stamp); e != nil {
			break
		}
		stored[id] = b
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = db.QueryContext(ctx, `SELECT revision,body FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType' IN ('3','4','5') AND NOT body ? 'commitType' AND document_id=$1||'#'||(body->>'uniqueId')`, s.uid())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var revision string
		var raw []byte
		if e = rows.Scan(&revision, &raw); e != nil {
			break
		}
		var b map[string]any
		if e = nativeJSON(raw, &b); e != nil {
			break
		}
		v.apply(b, revision, "")
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	for id, b := range v.Books {
		blob, ok := stored[id]
		if !ok {
			delete(v.Books, id)
			continue
		}
		b.SHA, b.Size, b.ContentType = blob.sha, blob.size, blob.ct
		if b.Modified == 0 {
			b.Modified = blob.stamp
		}
	}
	// A published change stays pending until the projection reaches its revision.
	rows, e = db.QueryContext(ctx, `SELECT o.id::text,o.kind,o.payload FROM boox_operation o LEFT JOIN boox_projection p ON p.document_id=o.document_id WHERE o.kind IN ('library_create','library_update','upload_book') AND o.payload->>'nativeUid'=$1 AND o.state NOT IN ('preview','blocked','cancelled','superseded') AND (o.state<>'published' OR o.updated_at>now()-interval '1 hour' AND (p.document_id IS NULL OR coalesce(p.body->>'alexandriaOperationId','')<>o.id::text AND CASE WHEN p.revision ~ '^[0-9]{1,9}-' THEN split_part(p.revision,'-',1)::int ELSE 0 END < CASE WHEN o.published_revision ~ '^[0-9]{1,9}-' THEN split_part(o.published_revision,'-',1)::int ELSE 0 END)) ORDER BY o.created_at,o.id`, s.uid())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		var raw []byte
		if e = rows.Scan(&id, &kind, &raw); e != nil {
			return nil, e
		}
		var p map[string]any
		if e = nativeJSON(raw, &p); e != nil {
			return nil, e
		}
		switch kind {
		case "upload_book":
			if m, ok := p["metadata"].(map[string]any); ok {
				v.apply(m, "", id)
				if b := v.Books[nativeString(m, "uniqueId")]; b != nil {
					b.SHA, b.Size, b.ContentType = nativeString(p, "sha256"), numberField(m, "size"), bookContentType(b.Kind)
				}
			}
		case "library_create":
			if m, ok := p["body"].(map[string]any); ok {
				v.apply(m, "", id)
			}
		case "library_update":
			v.change(nativeString(p, "mode"), nativeString(p, "id"), p["changes"], id)
		}
	}
	return v, rows.Err()
}

func numberField(m map[string]any, key string) int64 {
	n, _ := strconv.ParseInt(nativeString(m, key), 10, 64)
	return n
}

// apply records a native document. Books are kept only while a stored binding
// may exist; the caller drops the rest.
func (v *librarySnapshot) apply(b map[string]any, revision, head string) {
	id := nativeString(b, "uniqueId")
	live := nativeString(b, "status") == "0" || b["status"] == nil
	switch nativeString(b, "modeType") {
	case "3":
		v.Shelves[id] = &libraryShelf{ID: id, Parent: nativeString(b, "parentUniqueId"), Name: nativeString(b, "name"), Revision: revision, Head: head, Live: live, Native: nativeShelf(b)}
	case "4":
		// Cloud-account books are fetched from their provider, not stored here.
		if f := nativeString(b, "fetchSource"); f != "" && f != "0" {
			delete(v.Books, id)
			return
		}
		book := &libraryBook{ID: id, Name: nativeString(b, "name"), Title: nativeString(b, "title"), Kind: strings.ToLower(nativeString(b, "type")), Revision: revision, Head: head, Live: live, Modified: numberField(b, "updatedAt")}
		if old := v.Books[id]; old != nil && head != "" {
			book.SHA, book.Size, book.ContentType = old.SHA, old.Size, old.ContentType
		}
		v.Books[id] = book
	case "5":
		v.Members[id] = &libraryMember{ID: id, Book: nativeString(b, "documentUniqueId"), Shelf: nativeString(b, "libraryUniqueId"), Revision: revision, Head: head, Live: live}
	}
}
func (v *librarySnapshot) change(mode, id string, raw any, head string) {
	changes, _ := raw.(map[string]any)
	status := func(old bool) bool {
		if s, ok := changes["status"]; ok {
			return fmt.Sprint(s) == "0"
		}
		return old
	}
	text := func(key, old string) string {
		if c, ok := changes[key]; ok {
			s, _ := c.(string)
			return s
		}
		return old
	}
	switch mode {
	case "3":
		if f := v.Shelves[id]; f != nil {
			f.Name, f.Parent, f.Live, f.Head = text("name", f.Name), text("parentUniqueId", f.Parent), status(f.Live), head
		}
	case "4":
		if b := v.Books[id]; b != nil {
			b.Name, b.Live, b.Head = text("name", b.Name), status(b.Live), head
		}
	case "5":
		if m := v.Members[id]; m != nil {
			m.Shelf, m.Live, m.Head = text("libraryUniqueId", m.Shelf), status(m.Live), head
		}
	}
}

// Shelf returns a live shelf whose whole ancestry is live and native.
func (v *librarySnapshot) Shelf(id string) *libraryShelf {
	seen := map[string]bool{}
	for at := id; at != ""; {
		f := v.Shelves[at]
		if f == nil || !f.Live || !f.Native || seen[at] || len(seen) > 32 {
			return nil
		}
		seen[at] = true
		at = f.Parent
	}
	return v.Shelves[id]
}

// Membership answers the visible shelf memberships of a live book.
func (v *librarySnapshot) Memberships(book string) []*libraryMember {
	var out []*libraryMember
	for _, m := range v.Members {
		if m.Live && m.Book == book && v.Shelf(m.Shelf) != nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (v *librarySnapshot) membership(book, shelf string) *libraryMember {
	var found *libraryMember
	for _, m := range v.Members {
		if m.Book == book && m.Shelf == shelf && (found == nil || m.Live && !found.Live || m.Live == found.Live && m.ID < found.ID) {
			found = m
		}
	}
	return found
}
func (v *librarySnapshot) nameTaken(parent, name, except string) bool {
	for _, f := range v.Shelves {
		if f.Live && f.Parent == parent && f.Name == name && f.ID != except {
			return true
		}
	}
	return false
}
func (v *librarySnapshot) within(id, ancestor string) bool {
	seen := map[string]bool{}
	for at := id; at != "" && !seen[at] && len(seen) <= 32; {
		if at == ancestor {
			return true
		}
		seen[at] = true
		f := v.Shelves[at]
		if f == nil {
			return false
		}
		at = f.Parent
	}
	return false
}

func validLibraryName(name string) bool {
	return utf8.ValidString(name) && strings.TrimSpace(name) != "" && len(name) <= 240 && name != "." && name != ".." && !strings.ContainsAny(name, "/\\") && strings.IndexFunc(name, unicode.IsControl) < 0
}
func libraryNativeID(kind, token, key string) string {
	return strings.ReplaceAll(uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-library-"+kind+":"+token+":"+key)).String(), "-", "")
}
func nullableShelf(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// Native root and internal storage queries use IS NULL, not empty strings.
func readerShelfBody(uid, id, parent, name string, now int64) map[string]any {
	return map[string]any{"_id": uid + "#" + id, "uniqueId": id, "user": uid, "dbId": uid + "-READER_LIBRARY", "modeType": 3, "name": name, "parentUniqueId": nullableShelf(parent), "fetchSource": 0, "storageId": nil, "queryString": nil, "extraAttributes": "{}", "syncStatus": 1, "status": 0, "createdAt": now, "updatedAt": now}
}
func readerMembershipBody(uid, id, book, shelf string, now int64) map[string]any {
	return map[string]any{"_id": uid + "#" + id, "uniqueId": id, "user": uid, "dbId": uid + "-READER_LIBRARY", "modeType": 5, "documentUniqueId": book, "libraryUniqueId": shelf, "status": 0, "createdAt": now, "updatedAt": now}
}
func writableReaderShelf(b map[string]any, uid, id string) bool {
	return nativeReaderFolder(b, uid, id) && b["commitType"] == nil && nativeShelf(b)
}

// Cloud-account shelves are described "<Provider>-<account>" by the Library
// app (CreateNetDiskLibraryRequest; Provider enum of com.onyx 56184).
var netDiskShelf = regexp.MustCompile(`^(ONYX_NOTE|EVER_NOTE|YNOTE|DROP_BOX|ONE_NOTE|GOOGLE_DRIVE|BAIDU_YUN|ONE_DRIVE|NUT_STORE|WEBDAV|ALI_YUN|ZOTERO|NOTION|READWISE)-`)

// nativeShelf excludes remote, smart, storage and cloud-account shelves.
func nativeShelf(b map[string]any) bool {
	f := nativeString(b, "fetchSource")
	return (f == "" || f == "0") && nativeString(b, "queryString") == "" && nativeString(b, "storageId") == "" && !netDiskShelf.MatchString(nativeString(b, "description"))
}

type libraryOp struct {
	Kind, ID        string // library_create or library_update; native uniqueId
	Mode            int
	Expected, After string
	Changes, Body   map[string]any
}

// update fences on the projection revision, or on the pending change that
// will have written the document by the time this one is published.
func update(mode int, id, revision, head string, changes map[string]any) libraryOp {
	op := libraryOp{Kind: "library_update", ID: id, Mode: mode, Changes: changes}
	if head != "" {
		op.After = head
	} else {
		op.Expected = revision
	}
	return op
}

type libraryPlan func(v *librarySnapshot, now int64) ([]libraryOp, error)

func (s Service) libraryChange(ctx context.Context, token, action string, intent any, plan libraryPlan) error {
	if e := s.ResolveIdentity(ctx); e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	generation, e := s.publicationOwner(ctx, tx)
	if e != nil {
		return e
	}
	if e = s.libraryChangeTx(ctx, tx, generation, token, action, intent, plan); e != nil {
		return e
	}
	return tx.Commit()
}

// libraryChangeTx queues one plan atomically. Replaying a token with the same
// intent is a no-op; reusing it for anything else is refused.
func (s Service) libraryChangeTx(ctx context.Context, tx *sql.Tx, generation, token, action string, intent any, plan libraryPlan) error {
	if _, e := uuid.Parse(token); e != nil {
		return errLibraryChanged
	}
	raw, _ := json.Marshal(map[string]any{"action": action, "intent": intent})
	fingerprint := hash(raw)
	var previous string
	e := tx.QueryRowContext(ctx, `SELECT coalesce(payload->>'libraryFingerprint','') FROM boox_operation WHERE idempotency_key=$1`, "library:"+token+":0").Scan(&previous)
	if e == nil {
		if previous != fingerprint {
			return errLibraryChanged
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	v, e := s.librarySnapshot(ctx, tx)
	if e != nil {
		return e
	}
	now := time.Now().UnixMilli()
	ops, e := plan(v, now)
	if e != nil {
		return e
	}
	if len(ops) > libraryBatchLimit {
		return errLibraryTooLarge
	}
	touched := map[string]bool{}
	for i, op := range ops {
		if touched[op.ID] {
			return errors.New("library plan writes one record twice")
		}
		touched[op.ID] = true
		payload := map[string]any{"generation": generation, "nativeUid": s.uid(), "libraryBatch": token, "libraryFingerprint": fingerprint, "libraryAction": action, "id": op.ID, "mode": op.Mode, "updatedAt": now}
		if op.Kind == "library_create" {
			payload["body"] = op.Body
		} else {
			payload["changes"] = op.Changes
			if op.After != "" {
				payload["after"] = op.After
			}
		}
		p, _ := json.Marshal(payload)
		key := "library:" + token + ":" + strconv.Itoa(i)
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-"+key)).String()
		// clock_timestamp keeps the publication order of one plan.
		if _, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state,created_at) VALUES($1,$2,$3,$4,$5,$6,'queued',clock_timestamp())`, id, key, op.Kind, s.uid()+"#"+op.ID, op.Expected, p); e != nil {
			return e
		}
	}
	return nil
}

func (s Service) createShelf(ctx context.Context, token, parent, name string) (string, error) {
	id := libraryNativeID("shelf", token, "")
	return id, s.libraryChange(ctx, token, "create_shelf", []string{parent, name}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		if !validLibraryName(name) || parent == "" && name == libraryTrash {
			return nil, errLibraryName
		}
		if parent != "" && v.Shelf(parent) == nil {
			return nil, errLibraryChanged
		}
		if v.nameTaken(parent, name, "") {
			return nil, errLibraryConflict
		}
		return []libraryOp{{Kind: "library_create", ID: id, Mode: 3, Body: readerShelfBody(s.uid(), id, parent, name, now)}}, nil
	})
}
func (s Service) renameShelf(ctx context.Context, token, shelf, name string) error {
	return s.libraryChange(ctx, token, "rename_shelf", []string{shelf, name}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		f := v.Shelf(shelf)
		if f == nil {
			return nil, errLibraryChanged
		}
		if !validLibraryName(name) || f.Parent == "" && name == libraryTrash {
			return nil, errLibraryName
		}
		if f.Name == name {
			return nil, nil
		}
		if v.nameTaken(f.Parent, name, shelf) {
			return nil, errLibraryConflict
		}
		return []libraryOp{update(3, shelf, f.Revision, f.Head, map[string]any{"name": name})}, nil
	})
}

// moveShelf reparents, and renames as well when name is not empty.
func (s Service) moveShelf(ctx context.Context, token, shelf, parent, name string) error {
	return s.libraryChange(ctx, token, "move_shelf", []string{shelf, parent, name}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		f := v.Shelf(shelf)
		if f == nil || parent != "" && v.Shelf(parent) == nil {
			return nil, errLibraryChanged
		}
		if name == "" {
			name = f.Name
		}
		if !validLibraryName(name) || parent == "" && name == libraryTrash {
			return nil, errLibraryName
		}
		if v.within(parent, shelf) {
			return nil, errLibraryCycle
		}
		if f.Parent == parent && f.Name == name {
			return nil, nil
		}
		if v.nameTaken(parent, name, shelf) {
			return nil, errLibraryConflict
		}
		changes := map[string]any{"parentUniqueId": nullableShelf(parent)}
		if name != f.Name {
			changes["name"] = name
		}
		return []libraryOp{update(3, shelf, f.Revision, f.Head, changes)}, nil
	})
}

// deleteShelf is the Library app's non-recursive delete: memberships and
// non-empty child shelves move up one level, and no book is changed.
func (s Service) deleteShelf(ctx context.Context, token, shelf string) error {
	return s.libraryChange(ctx, token, "delete_shelf", []string{shelf}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		f := v.Shelf(shelf)
		if f == nil {
			return nil, errLibraryChanged
		}
		var ops []libraryOp
		var members []*libraryMember
		for _, m := range v.Members {
			if m.Live && m.Shelf == shelf {
				members = append(members, m)
			}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		for _, m := range members {
			changes := map[string]any{"status": 1}
			if p := v.membership(m.Book, f.Parent); f.Parent != "" && (p == nil || !p.Live) {
				changes = map[string]any{"libraryUniqueId": f.Parent}
			}
			ops = append(ops, update(5, m.ID, m.Revision, m.Head, changes))
		}
		var children []*libraryShelf
		for _, c := range v.Shelves {
			if c.Live && c.Parent == shelf {
				children = append(children, c)
			}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
		taken := map[string]bool{}
		for _, c := range v.Shelves {
			if c.Live && c.Parent == f.Parent && c.ID != shelf {
				taken[c.Name] = true
			}
		}
		for _, c := range children {
			empty := true
			for _, m := range v.Members {
				empty = empty && !(m.Live && m.Shelf == c.ID)
			}
			for _, g := range v.Shelves {
				empty = empty && !(g.Live && g.Parent == c.ID)
			}
			if empty {
				ops = append(ops, update(3, c.ID, c.Revision, c.Head, map[string]any{"status": 1}))
				continue
			}
			changes := map[string]any{"parentUniqueId": nullableShelf(f.Parent)}
			name := c.Name
			for n := 1; taken[name] || f.Parent == "" && name == libraryTrash; n++ {
				name = c.Name + "-" + strconv.Itoa(n)
			}
			if name != c.Name {
				changes["name"] = name
			}
			taken[name] = true
			ops = append(ops, update(3, c.ID, c.Revision, c.Head, changes))
		}
		return append(ops, update(3, shelf, f.Revision, f.Head, map[string]any{"status": 1})), nil
	})
}

func (s Service) renameBook(ctx context.Context, token, book, name string) error {
	return s.libraryChange(ctx, token, "rename_book", []string{book, name}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		b := v.Books[book]
		if b == nil || !b.Live {
			return nil, errLibraryChanged
		}
		ext := path.Ext(b.Name)
		if b.Kind != "" {
			ext = "." + b.Kind
		}
		if !validLibraryName(name) || !strings.EqualFold(path.Ext(name), ext) || strings.TrimSuffix(name, path.Ext(name)) == "" {
			return nil, errLibraryName
		}
		if b.Name == name {
			return nil, nil
		}
		return []libraryOp{update(4, book, b.Revision, b.Head, map[string]any{"name": name})}, nil
	})
}

func (v *librarySnapshot) shelve(uid, token, book, shelf string, now int64) []libraryOp {
	m := v.membership(book, shelf)
	if m != nil && m.Live {
		return nil
	}
	if m != nil {
		return []libraryOp{update(5, m.ID, m.Revision, m.Head, map[string]any{"status": 0})}
	}
	id := libraryNativeID("membership", token, book+":"+shelf)
	return []libraryOp{{Kind: "library_create", ID: id, Mode: 5, Body: readerMembershipBody(uid, id, book, shelf, now)}}
}

// onlyShelf shelves an unshelved book. A book already on this shelf is left
// alone; one on any other shelf is refused.
func (v *librarySnapshot) onlyShelf(uid, token, book, shelf string, now int64) ([]libraryOp, error) {
	members := v.Memberships(book)
	for _, m := range members {
		if m.Shelf == shelf {
			return nil, nil
		}
	}
	if len(members) > 0 {
		return nil, errLibraryShelves
	}
	if shelf == "" {
		return nil, nil
	}
	return v.shelve(uid, token, book, shelf, now), nil
}

func (s Service) addBookToShelf(ctx context.Context, token, book, shelf string) error {
	return s.libraryChange(ctx, token, "add_book", []string{book, shelf}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		if b := v.Books[book]; b == nil || !b.Live || v.Shelf(shelf) == nil {
			return nil, errLibraryChanged
		}
		return v.onlyShelf(s.uid(), token, book, shelf, now)
	})
}

// moveBook moves between shelves, where "" is the top level of unshelved books.
// A move between shelves updates the membership in place, as the device does.
func (s Service) moveBook(ctx context.Context, token, book, from, to string) error {
	return s.libraryChange(ctx, token, "move_book", []string{book, from, to}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		return v.planMove(s.uid(), token, book, from, to, now)
	})
}
func (v *librarySnapshot) planMove(uid, token, book, from, to string, now int64) ([]libraryOp, error) {
	b := v.Books[book]
	if b == nil || !b.Live || to != "" && v.Shelf(to) == nil {
		return nil, errLibraryChanged
	}
	members := v.Memberships(book)
	if from == "" {
		if len(members) > 0 {
			return nil, errLibraryChanged
		}
		if to == "" {
			return nil, nil
		}
		return v.shelve(uid, token, book, to, now), nil
	}
	m := v.membership(book, from)
	if m == nil || !m.Live || v.Shelf(from) == nil {
		return nil, errLibraryChanged
	}
	if from == to {
		return nil, nil
	}
	if to == "" {
		if len(members) > 1 {
			return nil, errLibraryShelves
		}
		return []libraryOp{update(5, m.ID, m.Revision, m.Head, map[string]any{"libraryUniqueId": nil, "status": 1})}, nil
	}
	if t := v.membership(book, to); t != nil && t.Live {
		return []libraryOp{update(5, m.ID, m.Revision, m.Head, map[string]any{"status": 1})}, nil
	}
	return []libraryOp{update(5, m.ID, m.Revision, m.Head, map[string]any{"libraryUniqueId": to})}, nil
}

// removeBook is a file-manager delete: the book is trashed the way the Library
// app removes it from the library while a shelf is open. A legacy book with
// several memberships only leaves the one shelf.
func (s Service) removeBook(ctx context.Context, token, book, from string) error {
	return s.libraryChange(ctx, token, "remove_book", []string{book, from}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		return v.planRemove(book, from)
	})
}
func (v *librarySnapshot) planRemove(book, from string) ([]libraryOp, error) {
	b := v.Books[book]
	if b == nil || !b.Live {
		return nil, errLibraryChanged
	}
	members := v.Memberships(book)
	trash := update(4, book, b.Revision, b.Head, map[string]any{"status": 1})
	if from == "" {
		if len(members) > 0 {
			return nil, errLibraryChanged
		}
		return []libraryOp{trash}, nil
	}
	m := v.membership(book, from)
	if m == nil || !m.Live || v.Shelf(from) == nil {
		return nil, errLibraryChanged
	}
	leave := update(5, m.ID, m.Revision, m.Head, map[string]any{"status": 1})
	if len(members) > 1 {
		return []libraryOp{leave}, nil
	}
	return []libraryOp{trash, leave}, nil
}

// uploadBook queues a new book, and its shelf when shelf is not empty, in one
// transaction. Re-sending identical bytes under the same name to the same place
// succeeds as a retry; identical bytes under another name, or bound for
// another shelf, are the existing book and are refused.
func (s Service) uploadBook(ctx context.Context, token, name string, raw []byte, shelf string) error {
	_, e := s.queueBook(ctx, name, raw, func(tx *sql.Tx, generation, operation string) error {
		var book, queued string
		if e := tx.QueryRowContext(ctx, `SELECT payload->'metadata'->>'uniqueId',payload->>'name' FROM boox_operation WHERE id=$1`, operation).Scan(&book, &queued); e != nil {
			return e
		}
		if queued != name {
			return errBookConflict
		}
		return s.libraryChangeTx(ctx, tx, generation, token, "upload_book", []string{book, shelf}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
			if b := v.Books[book]; b == nil || !b.Live || shelf != "" && v.Shelf(shelf) == nil {
				return nil, errLibraryChanged
			}
			return v.onlyShelf(s.uid(), token, book, shelf, now)
		})
	})
	return e
}

// restoreBook brings a trashed stored book back, optionally onto a shelf. A
// book the Library app removed from All Books may still hold its membership;
// it is moved rather than joined by a second one.
func (s Service) restoreBook(ctx context.Context, token, book, to string) error {
	return s.libraryChange(ctx, token, "restore_book", []string{book, to}, func(v *librarySnapshot, now int64) ([]libraryOp, error) {
		return v.planRestore(s.uid(), token, book, to, now)
	})
}
func (v *librarySnapshot) planRestore(uid, token, book, to string, now int64) ([]libraryOp, error) {
	b := v.Books[book]
	if b == nil || b.Live || to != "" && v.Shelf(to) == nil {
		return nil, errLibraryChanged
	}
	ops := []libraryOp{update(4, book, b.Revision, b.Head, map[string]any{"status": 0})}
	members := v.Memberships(book)
	for i, m := range members {
		switch {
		case i == 0 && m.Shelf == to:
		case i == 0 && to != "":
			ops = append(ops, update(5, m.ID, m.Revision, m.Head, map[string]any{"libraryUniqueId": to}))
		default:
			ops = append(ops, update(5, m.ID, m.Revision, m.Head, map[string]any{"status": 1}))
		}
	}
	if to != "" && len(members) == 0 {
		ops = append(ops, v.shelve(uid, token, book, to, now)...)
	}
	return ops, nil
}

func libraryDocument(b map[string]any, uid, mode string) bool {
	return b["user"] == uid && b["dbId"] == uid+"-READER_LIBRARY" && b["_id"] == uid+"#"+nativeString(b, "uniqueId") && nativeString(b, "modeType") == mode && b["commitType"] == nil
}

// libraryReferences re-checks, against the Gateway, the shelves and book a
// live record will point at. It answers a block code, or "" when publishable.
func (s Service) libraryReferences(ctx context.Context, body map[string]any, created bool) (string, error) {
	if nativeString(body, "status") != "0" {
		return "", nil
	}
	get := func(id string) (map[string]any, error) {
		var b map[string]any
		e := s.Config.gateway(ctx, "GET", "/"+url.PathEscape(s.uid()+"#"+id), nil, &b)
		var u UpstreamError
		if errors.As(e, &u) && u.Status == 404 {
			return nil, nil
		}
		return b, e
	}
	switch nativeString(body, "modeType") {
	case "5":
		if created {
			book := nativeString(body, "documentUniqueId")
			b, e := get(book)
			if e != nil {
				return "", e
			}
			if b == nil || !libraryDocument(b, s.uid(), "4") || nativeString(b, "status") != "0" {
				return "book_changed", nil
			}
		}
		shelf := nativeString(body, "libraryUniqueId")
		if shelf == "" {
			return "", nil
		}
		f, e := get(shelf)
		if e != nil {
			return "", e
		}
		if f == nil || !writableReaderShelf(f, s.uid(), shelf) {
			return "destination_changed", nil
		}
	case "3":
		self := nativeString(body, "uniqueId")
		for at, depth := nativeString(body, "parentUniqueId"), 0; at != ""; depth++ {
			if at == self {
				return "shelf_cycle", nil
			}
			f, e := get(at)
			if e != nil {
				return "", e
			}
			if f == nil || !writableReaderShelf(f, s.uid(), at) || depth > 32 {
				return "destination_changed", nil
			}
			at = nativeString(f, "parentUniqueId")
		}
	}
	return "", nil
}

// publishLibrary advances one library change with the operation locked and the
// generation fenced. Library tree changes notify devices with a READER_LIBRARY
// message only, as book imports do; they carry no reader commit.
func (s Service) publishLibrary(ctx context.Context, tx *sql.Tx, id, kind, doc, expected, state string, payload map[string]any) error {
	block := func(code string) error {
		_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='blocked',error_code=$2,updated_at=now() WHERE id=$1`, id, code)
		if e == nil {
			e = tx.Commit()
		}
		return e
	}
	mode := nativeString(payload, "mode")
	var live map[string]any
	read := func() error {
		live = nil
		return s.Config.gateway(ctx, "GET", "/"+url.PathEscape(doc), nil, &live)
	}
	if state == "queued" {
		if kind == "library_create" {
			body, ok := payload["body"].(map[string]any)
			if !ok || !libraryDocument(body, s.uid(), mode) || body["_id"] != doc {
				return errors.New("invalid library publication")
			}
			code, e := s.libraryReferences(ctx, body, true)
			if e != nil || code != "" {
				if e != nil {
					return e
				}
				return block(code)
			}
			body["alexandriaOperationId"] = id
			if e = s.putIdempotent(ctx, doc, id, body); errors.Is(e, errPublicationCollision) {
				return block("identity_in_use")
			} else if e != nil {
				return e
			}
		} else {
			if e := read(); e != nil {
				var u UpstreamError
				if errors.As(e, &u) && u.Status == 404 {
					return block("stale_preview")
				}
				return e
			}
			if live["alexandriaOperationId"] != id {
				// Devices keep unknown fields when they rewrite a record, so a chained
				// change fences on the exact revision its predecessor published, not
				// on the predecessor's marker.
				after := nativeString(payload, "after")
				fence := expected
				if after != "" {
					var state string
					var published sql.NullString
					if e := tx.QueryRowContext(ctx, `SELECT state,published_revision FROM boox_operation WHERE id=$1`, after).Scan(&state, &published); e != nil && !errors.Is(e, sql.ErrNoRows) {
						return e
					}
					if state == "queued" && !published.Valid {
						return errors.New("earlier library change not yet published")
					}
					fence = published.String
				}
				if !libraryDocument(live, s.uid(), mode) || fence == "" || live["_rev"] != fence {
					return block("stale_preview")
				}
				changes, ok := payload["changes"].(map[string]any)
				if !ok || len(changes) == 0 {
					return errors.New("missing library changes")
				}
				for k, v := range changes {
					live[k] = v
				}
				code, e := s.libraryReferences(ctx, live, false)
				if e != nil || code != "" {
					if e != nil {
						return e
					}
					return block(code)
				}
				live["updatedAt"] = payload["updatedAt"]
				live["alexandriaOperationId"] = id
				delete(live, "_revisions")
				if e = s.Config.gateway(ctx, "PUT", "/"+url.PathEscape(doc), live, nil); e != nil {
					return e
				}
			}
		}
		if e := read(); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='record_published',published_revision=$2,updated_at=now() WHERE id=$1`, id, live["_rev"]); e != nil {
			return e
		}
	} else if e := read(); e != nil {
		return e
	}
	if live["alexandriaOperationId"] != id {
		_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='superseded',error_code='native_edit_after_publication' WHERE id=$1`, id)
		if e == nil {
			e = tx.Commit()
		}
		return e
	}
	messageID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-message:"+id)).String()
	message := map[string]any{"_id": messageID, "uniqueId": messageID, "user": s.uid(), "dbId": s.uid() + "-MESSAGE", "msgType": 6, "replicatorName": "READER_LIBRARY", "syncAt": payload["updatedAt"], "msgStatus": 0, "clusterLang": "powersync_" + strings.ReplaceAll(s.LibraryID, "-", "")[:16], "createdAt": payload["updatedAt"], "updatedAt": payload["updatedAt"], "alexandriaOperationId": id}
	if e := s.putIdempotent(ctx, messageID, id, message); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `UPDATE boox_operation SET state='published',updated_at=now() WHERE id=$1`, id)
	if e == nil {
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_receipt(operation_id,device_id) SELECT $1,id FROM boox_device WHERE NOT revoked ON CONFLICT DO NOTHING`, id)
	}
	if e == nil {
		e = tx.Commit()
	}
	return e
}
