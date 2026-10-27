package boox

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-webdav"
	"github.com/google/uuid"
)

// DAVPrefix mounts the stored-book file view for WebDAV clients. Every write
// goes through the same journaled library changes as the Reading page.
const DAVPrefix = "/dav/books"

func OwnsDAV(p string) bool { return p == DAVPrefix || strings.HasPrefix(p, DAVPrefix+"/") }

// DAV serves WebDAV. go-webdav has no class 2 support, so LOCK, UNLOCK and
// PROPPATCH are answered here: locks are advisory tokens that are never
// enforced (every write is already revision-fenced), and property writes such
// as Explorer's Win32 timestamps are accepted and ignored.
func (s Service) DAV(w http.ResponseWriter, r *http.Request) {
	fs := &libraryFS{s: s}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("DAV", "1, 2, 3")
		w.Header().Set("MS-Author-Via", "DAV")
		w.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, COPY, MOVE, LOCK, UNLOCK")
		w.WriteHeader(http.StatusOK)
		return
	case "LOCK":
		fs.lock(w, r)
		return
	case "UNLOCK":
		w.WriteHeader(http.StatusNoContent)
		return
	case "PROPPATCH":
		fs.proppatch(w, r)
		return
	}
	(&webdav.Handler{FileSystem: fs}).ServeHTTP(w, r)
}

// Explorer creates an empty file before writing its bytes, and LOCK on an
// unmapped URL creates one too. Placeholders live only in this process, expire,
// and never become library records.
const davPlaceholderTTL = 10 * time.Minute

type davPlaceholderKey struct{ library, folder, name string }

var davPlaceholders = struct {
	sync.Mutex
	at map[davPlaceholderKey]time.Time
}{at: map[davPlaceholderKey]time.Time{}}

func (f *libraryFS) placeholders(folder string) []string {
	davPlaceholders.Lock()
	defer davPlaceholders.Unlock()
	var out []string
	for k, at := range davPlaceholders.at {
		if time.Since(at) > davPlaceholderTTL {
			delete(davPlaceholders.at, k)
		} else if k.library == f.s.LibraryID && k.folder == folder {
			out = append(out, k.name)
		}
	}
	return out
}
func (f *libraryFS) setPlaceholder(folder, name string, present bool) {
	davPlaceholders.Lock()
	defer davPlaceholders.Unlock()
	k := davPlaceholderKey{f.s.LibraryID, folder, name}
	if present {
		davPlaceholders.at[k] = time.Now()
	} else {
		delete(davPlaceholders.at, k)
	}
}

// Desktop metadata is accepted and discarded so that copying a folder does
// not fail on files nobody meant to put in a library.
func davJunk(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, "._") || name == ".DS_Store" || name == ".localized" || lower == "thumbs.db" || lower == "desktop.ini"
}

type libraryFS struct {
	s     Service
	index *libraryIndex
}

var _ webdav.FileSystem = (*libraryFS)(nil)

// davCause carries a status through go-webdav's internal error type, which
// unwraps to it.
type davCause struct {
	code    int
	message string
}

func (c davCause) Error() string { return c.message }
func davStatus(code int, message string) error {
	return webdav.NewHTTPError(code, davCause{code, message})
}

func (f *libraryFS) relative(name string) (string, error) {
	if !OwnsDAV(name) || strings.Contains(name, "\x00") {
		return "", davStatus(http.StatusNotFound, "not in the book library")
	}
	return path.Clean("/" + strings.TrimPrefix(name, DAVPrefix)), nil
}
func (f *libraryFS) load(ctx context.Context) (*libraryIndex, error) {
	if f.index == nil {
		if e := f.s.ResolveIdentity(ctx); e != nil {
			return nil, e
		}
		v, e := f.s.librarySnapshot(ctx, f.s.DB)
		if e != nil {
			return nil, e
		}
		f.index = newLibraryIndex(v)
	}
	return f.index, nil
}
func (f *libraryFS) lookup(ctx context.Context, name string) (string, libraryEntry, bool, error) {
	p, e := f.relative(name)
	if e != nil {
		return "", libraryEntry{}, false, e
	}
	x, e := f.load(ctx)
	if e != nil {
		return p, libraryEntry{}, false, e
	}
	entry, found := x.Lookup(p, f.placeholders)
	return p, entry, found, nil
}

// parent resolves the folder a new path would be created in.
func (f *libraryFS) parent(ctx context.Context, name string) (string, libraryEntry, string, error) {
	p, e := f.relative(name)
	if e != nil {
		return "", libraryEntry{}, "", e
	}
	if p == "/" {
		return p, libraryEntry{}, "", davStatus(http.StatusForbidden, "the library root cannot be replaced")
	}
	x, e := f.load(ctx)
	if e != nil {
		return p, libraryEntry{}, "", e
	}
	folder, found := x.Lookup(path.Dir(p), f.placeholders)
	if !found || !folder.Dir() {
		return p, libraryEntry{}, "", davStatus(http.StatusConflict, "the destination folder does not exist")
	}
	return p, folder, path.Base(p), nil
}

func (f *libraryFS) info(p string, e libraryEntry) *webdav.FileInfo {
	href := DAVPrefix + p
	if e.Dir() {
		return &webdav.FileInfo{Path: strings.TrimSuffix(href, "/") + "/", IsDir: true}
	}
	if e.Placeholder {
		return &webdav.FileInfo{Path: href, MIMEType: mime.TypeByExtension(path.Ext(p)), ETag: "placeholder"}
	}
	b := e.Book
	ct := b.ContentType
	if ct == "" {
		ct = mime.TypeByExtension(path.Ext(e.Name))
	}
	return &webdav.FileInfo{Path: href, Size: b.Size, ModTime: time.UnixMilli(b.Modified), MIMEType: ct, ETag: b.SHA}
}

func (f *libraryFS) Stat(ctx context.Context, name string) (*webdav.FileInfo, error) {
	p, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return nil, f.fail(e)
	}
	if !found {
		return nil, davStatus(http.StatusNotFound, "not found")
	}
	return f.info(p, entry), nil
}

func (f *libraryFS) ReadDir(ctx context.Context, name string, recursive bool) ([]webdav.FileInfo, error) {
	p, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return nil, f.fail(e)
	}
	if !found {
		return nil, davStatus(http.StatusNotFound, "not found")
	}
	if !entry.Dir() {
		return []webdav.FileInfo{*f.info(p, entry)}, nil
	}
	out := []webdav.FileInfo{*f.info(p, entry)}
	type pending struct {
		path string
		key  string
	}
	queue := []pending{{p, entry.Key}}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, child := range f.index.Children(at.key, f.placeholders(at.key)) {
			cp := path.Join(at.path, child.Name)
			out = append(out, *f.info(cp, child))
			if recursive && child.Dir() {
				queue = append(queue, pending{cp, child.Key})
			}
		}
		if len(out) > 20000 {
			return nil, davStatus(http.StatusForbidden, "listing too large; use Depth: 1")
		}
	}
	return out, nil
}

type davReader struct{ *bytes.Reader }

func (davReader) Close() error { return nil }

func (f *libraryFS) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	_, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return nil, f.fail(e)
	}
	if !found || entry.Dir() {
		return nil, davStatus(http.StatusNotFound, "not found")
	}
	if entry.Placeholder {
		return davReader{bytes.NewReader(nil)}, nil
	}
	body, _, e := f.s.Objects.Get(ctx, "boox/bodies/"+entry.Book.SHA)
	if e != nil {
		return nil, f.fail(e)
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, maxObject+1))
	if e != nil || int64(len(raw)) != entry.Book.Size || hash(raw) != entry.Book.SHA {
		return nil, f.fail(errors.New("stored book bytes unavailable or corrupt"))
	}
	return davReader{bytes.NewReader(raw)}, nil
}

func (f *libraryFS) Create(ctx context.Context, name string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	p, folder, leaf, e := f.parent(ctx, name)
	if e != nil {
		return nil, false, f.fail(e)
	}
	if folder.Key == libraryTrashKey {
		return nil, false, davStatus(http.StatusForbidden, "books cannot be uploaded into .Trash")
	}
	if davJunk(leaf) {
		_, _ = io.Copy(io.Discard, io.LimitReader(body, maxObject))
		return &webdav.FileInfo{Path: DAVPrefix + p}, true, nil
	}
	existing, found := f.index.find(f.index.Children(folder.Key, f.placeholders(folder.Key)), leaf)
	if found && opts != nil && opts.IfNoneMatch.IsWildcard() {
		return nil, false, davStatus(http.StatusPreconditionFailed, "already exists")
	}
	raw, e := io.ReadAll(io.LimitReader(body, maxObject+1))
	if e != nil {
		return nil, false, davStatus(http.StatusBadRequest, "could not read the upload")
	}
	if len(raw) > maxObject {
		return nil, false, davStatus(http.StatusRequestEntityTooLarge, "books are limited to 64 MiB")
	}
	if found && !existing.Placeholder {
		// Re-sending the same bytes is a retry; anything else would replace a book.
		if hash(raw) == existing.Book.SHA {
			return f.info(p, existing), false, nil
		}
		return nil, false, davStatus(http.StatusConflict, "books are never overwritten; upload under a new name")
	}
	if len(raw) == 0 {
		f.setPlaceholder(folder.Key, leaf, true)
		return &webdav.FileInfo{Path: DAVPrefix + p, ETag: "placeholder"}, !found, nil
	}
	if _, e = validateBook(leaf, raw); e != nil {
		return nil, false, davStatus(http.StatusUnsupportedMediaType, e.Error())
	}
	if e = f.s.uploadBook(ctx, uuid.NewString(), leaf, raw, folder.Key); e != nil {
		return nil, false, f.fail(e)
	}
	f.setPlaceholder(folder.Key, leaf, false)
	f.index = nil
	return &webdav.FileInfo{Path: DAVPrefix + p, Size: int64(len(raw)), MIMEType: mime.TypeByExtension(path.Ext(leaf)), ETag: hash(raw)}, true, nil
}

func (f *libraryFS) RemoveAll(ctx context.Context, name string, opts *webdav.RemoveAllOptions) error {
	_, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return f.fail(e)
	}
	if !found {
		return davStatus(http.StatusNotFound, "not found")
	}
	token := uuid.NewString()
	switch {
	case entry.Placeholder:
		f.setPlaceholder(entry.Folder, entry.Name, false)
		return nil
	case entry.Key == "" && entry.Dir() || entry.Key == libraryTrashKey:
		return davStatus(http.StatusForbidden, "this folder cannot be removed")
	case entry.Folder == libraryTrashKey:
		return davStatus(http.StatusForbidden, "permanent removal is not available over WebDAV")
	case entry.Dir():
		e = f.s.deleteShelf(ctx, token, entry.Key)
	default:
		if opts != nil && opts.IfMatch.IsSet() {
			if ok, _ := opts.IfMatch.MatchETag(entry.Book.SHA); !ok {
				return davStatus(http.StatusPreconditionFailed, "the book changed")
			}
		}
		e = f.s.removeBook(ctx, token, entry.Book.ID, entry.Folder)
	}
	f.index = nil
	return f.fail(e)
}

func (f *libraryFS) Mkdir(ctx context.Context, name string) error {
	p, folder, leaf, e := f.parent(ctx, name)
	if e != nil {
		return f.fail(e)
	}
	if _, found := f.index.Lookup(p, f.placeholders); found {
		return davStatus(http.StatusMethodNotAllowed, "already exists")
	}
	if folder.Key == libraryTrashKey {
		return davStatus(http.StatusForbidden, "folders cannot be created in .Trash")
	}
	_, e = f.s.createShelf(ctx, uuid.NewString(), folder.Key, leaf)
	f.index = nil
	if errors.Is(e, errLibraryConflict) {
		return davStatus(http.StatusMethodNotAllowed, "already exists")
	}
	return f.fail(e)
}

// Copy is refused: devices keep each book on one shelf, and a book's bytes
// are never duplicated. Copying a book onto itself is a no-op.
func (f *libraryFS) Copy(ctx context.Context, name, dest string, opts *webdav.CopyOptions) (bool, error) {
	_, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return false, f.fail(e)
	}
	if !found {
		return false, davStatus(http.StatusNotFound, "not found")
	}
	target, err := f.relative(dest)
	if err != nil {
		return false, err
	}
	if existing, exists := f.index.Lookup(target, f.placeholders); exists && entry.Book != nil && existing.Book != nil && existing.Book.ID == entry.Book.ID {
		return false, nil
	}
	return false, davStatus(http.StatusForbidden, errLibraryShelves.Error())
}

func (f *libraryFS) Move(ctx context.Context, name, dest string, opts *webdav.MoveOptions) (bool, error) {
	_, entry, found, e := f.lookup(ctx, name)
	if e != nil {
		return false, f.fail(e)
	}
	if !found {
		return false, davStatus(http.StatusNotFound, "not found")
	}
	if entry.Dir() && (entry.Key == "" || entry.Key == libraryTrashKey) {
		return false, davStatus(http.StatusForbidden, "this folder cannot be moved")
	}
	target, folder, leaf, e := f.parent(ctx, dest)
	if e != nil {
		return false, f.fail(e)
	}
	if existing, exists := f.index.Lookup(target, f.placeholders); exists {
		same := existing.Folder == entry.Folder && existing.Name == entry.Name
		if !same {
			if opts != nil && opts.NoOverwrite {
				return false, os.ErrExist
			}
			if !existing.Placeholder {
				return false, davStatus(http.StatusForbidden, "replacing an item is not supported")
			}
			f.setPlaceholder(existing.Folder, existing.Name, false)
		}
	}
	defer func() { f.index = nil }()
	switch {
	case entry.Placeholder:
		if folder.Key == libraryTrashKey {
			return false, davStatus(http.StatusForbidden, "books cannot be moved into .Trash")
		}
		f.setPlaceholder(entry.Folder, entry.Name, false)
		f.setPlaceholder(folder.Key, leaf, true)
		return true, nil
	case entry.Dir():
		if folder.Key == libraryTrashKey {
			return false, davStatus(http.StatusForbidden, "delete the folder instead; its contents move up a level")
		}
		return true, f.fail(f.s.moveShelf(ctx, uuid.NewString(), entry.Key, folder.Key, leaf))
	case folder.Key == libraryTrashKey:
		if entry.Folder == libraryTrashKey {
			return false, nil
		}
		return true, f.fail(f.s.removeBook(ctx, uuid.NewString(), entry.Book.ID, entry.Folder))
	case entry.Folder == libraryTrashKey:
		e = f.s.restoreBook(ctx, uuid.NewString(), entry.Book.ID, folder.Key)
	case entry.Folder != folder.Key:
		e = f.s.moveBook(ctx, uuid.NewString(), entry.Book.ID, entry.Folder, folder.Key)
	}
	if e == nil && leaf != entry.Name {
		e = f.s.renameBook(ctx, uuid.NewString(), entry.Book.ID, leaf)
	}
	return true, f.fail(e)
}

// fail maps library refusals to WebDAV statuses and hides internal errors.
func (f *libraryFS) fail(e error) error {
	var status davCause
	switch {
	case e == nil:
		return nil
	case errors.As(e, &status):
		return e
	case errors.Is(e, errLibraryName):
		return davStatus(http.StatusForbidden, "names must be 1–240 bytes without slashes or control characters, and books keep their extension")
	case errors.Is(e, errLibraryCycle):
		return davStatus(http.StatusForbidden, e.Error())
	case errors.Is(e, errLibraryTooLarge):
		return davStatus(http.StatusForbidden, e.Error())
	case errors.Is(e, errLibraryConflict), errors.Is(e, errLibraryShelves), errors.Is(e, errLibraryChanged):
		return davStatus(http.StatusConflict, e.Error())
	case errors.Is(e, errBookConflict):
		return davStatus(http.StatusConflict, "this file is already in the library under another name")
	case errors.Is(e, errBookRemoved):
		return davStatus(http.StatusConflict, errBookRemoved.Error())
	case errors.Is(e, errBookEnrollment):
		return davStatus(http.StatusServiceUnavailable, errBookEnrollment.Error())
	}
	if f.s.Logger != nil {
		f.s.Logger.Error("BOOX WebDAV operation failed", "error", e)
	}
	return davStatus(http.StatusInternalServerError, "the library is temporarily unavailable")
}

func (f *libraryFS) href(p string) string {
	return xmlText((&url.URL{Path: DAVPrefix + p}).EscapedPath())
}
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// lock answers an advisory exclusive lock. LOCK on an unmapped URL creates an
// empty placeholder, as RFC 4918 requires.
func (f *libraryFS) lock(w http.ResponseWriter, r *http.Request) {
	p, _, found, e := f.lookup(r.Context(), r.URL.Path)
	if e != nil {
		f.serveError(w, f.fail(e))
		return
	}
	status := http.StatusOK
	if !found {
		_, folder, leaf, pe := f.parent(r.Context(), r.URL.Path)
		if pe != nil {
			f.serveError(w, f.fail(pe))
			return
		}
		if folder.Key == libraryTrashKey || davJunk(leaf) {
			f.serveError(w, davStatus(http.StatusForbidden, "cannot lock here"))
			return
		}
		f.setPlaceholder(folder.Key, leaf, true)
		status = http.StatusCreated
	}
	depth := "infinity"
	if r.Header.Get("Depth") == "0" {
		depth = "0"
	}
	token := "opaquelocktoken:" + uuid.NewString()
	if existing := strings.Trim(r.Header.Get("If"), "()<> "); strings.HasPrefix(existing, "opaquelocktoken:") {
		token = existing // a refresh keeps its token
	}
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.Header().Set("Lock-Token", "<"+token+">")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><D:prop xmlns:D="DAV:"><D:lockdiscovery><D:activelock><D:locktype><D:write/></D:locktype><D:lockscope><D:exclusive/></D:lockscope><D:depth>%s</D:depth><D:timeout>Second-3600</D:timeout><D:locktoken><D:href>%s</D:href></D:locktoken><D:lockroot><D:href>%s</D:href></D:lockroot></D:activelock></D:lockdiscovery></D:prop>`, depth, xmlText(token), f.href(p))
}

// proppatch accepts every property change without storing it.
func (f *libraryFS) proppatch(w http.ResponseWriter, r *http.Request) {
	p, _, found, e := f.lookup(r.Context(), r.URL.Path)
	if e != nil {
		f.serveError(w, f.fail(e))
		return
	}
	if !found {
		f.serveError(w, davStatus(http.StatusNotFound, "not found"))
		return
	}
	var props []xml.Name
	d := xml.NewDecoder(io.LimitReader(r.Body, 1<<20))
	depth, inProp := 0, 0
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			f.serveError(w, davStatus(http.StatusBadRequest, "invalid PROPPATCH body"))
			return
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
			if inProp == 0 && t.Name.Space == "DAV:" && t.Name.Local == "prop" {
				inProp = depth
			} else if inProp > 0 && depth == inProp+1 {
				props = append(props, t.Name)
			}
		case xml.EndElement:
			if depth == inProp {
				inProp = 0
			}
			depth--
		}
		if len(props) > 256 {
			break
		}
	}
	var b strings.Builder
	for i, n := range props {
		fmt.Fprintf(&b, `<p%d:%s xmlns:p%d="%s"/>`, i, xmlText(n.Local), i, xmlText(n.Space))
	}
	w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusMultiStatus)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:"><D:response><D:href>%s</D:href><D:propstat><D:prop>%s</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`, f.href(p), b.String())
}

func (f *libraryFS) serveError(w http.ResponseWriter, e error) {
	status := davCause{http.StatusInternalServerError, "the library is temporarily unavailable"}
	errors.As(e, &status)
	http.Error(w, status.message, status.code)
}
