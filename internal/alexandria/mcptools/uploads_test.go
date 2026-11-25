package mcptools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fileHost answers every address from a table of path to body; a body
// starting "disposition:" is served under that file name.
func fileHost(files map[string]string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}
		if body, ok := files[r.URL.Path]; ok {
			if name, rest, named := strings.Cut(strings.TrimPrefix(body, "disposition:"), "\n"); named && strings.HasPrefix(body, "disposition:") {
				resp.Header.Set("Content-Disposition", `attachment; filename="`+name+`"`)
				body = rest
			}
			resp.StatusCode, resp.Body, resp.ContentLength = 200, io.NopCloser(strings.NewReader(body)), int64(len(body))
		}
		return resp, nil
	})}
}

func TestUploadBook(t *testing.T) {
	var books []string
	db, s := setup(t, options{booxLibrary: fakeBOOX{books: &books}})
	var out addBookOutput
	text := call(t, s, "upload_book", map[string]any{"name": "Summary.md", "text": "# Garden\nPlant garlic.", "shelf": "Papers"}, &out)
	if out.Book != "boox:ps_x#b1" || out.Name != "Summary.md" || out.Shelf != "Work / Papers" || out.Bytes != 22 || out.State != "queued" ||
		out.SHA256 != sum([]byte("# Garden\nPlant garlic.")) || out.URL != "https://library.example/boox/reading?selected=ps_x%23b1" ||
		!strings.Contains(text, "Queued Summary.md (22 bytes) on the shelf Work / Papers") || len(books) != 1 || books[0] != "Papers|Summary.md|# Garden\nPlant garlic." {
		t.Fatal(text, out, books)
	}
	// The record keeps what was sent, never the file.
	var args string
	if err := db.QueryRowContext(ctx, `SELECT arguments FROM alexandria_mcp_change WHERE tool='upload_book'`).Scan(&args); err != nil ||
		strings.Contains(args, "garlic") || !strings.Contains(args, `"bytes":22`) || !strings.Contains(args, out.SHA256) || !strings.Contains(args, `"name":"Summary.md"`) {
		t.Fatal(args, err)
	}

	pdf := []byte("%PDF-1.4\n\x00\xff synthetic\n%%EOF\n")
	// Wrapped lines, the URL alphabet and missing padding all decode.
	for _, sent := range []string{base64.StdEncoding.EncodeToString(pdf), base64.RawURLEncoding.EncodeToString(pdf),
		base64.StdEncoding.EncodeToString(pdf)[:20] + "\n " + base64.StdEncoding.EncodeToString(pdf)[20:]} {
		books = nil
		text = call(t, s, "upload_book", map[string]any{"name": "Guide.pdf", "content_base64": sent, "sha256": strings.ToUpper(sum(pdf))}, &out)
		if len(books) != 1 || books[0] != "|Guide.pdf|"+string(pdf) || out.Shelf != "" || !strings.Contains(text, "with no shelf") {
			t.Fatal(text, books)
		}
	}
	books = nil
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "Guide.pdf", "content_base64": base64.StdEncoding.EncodeToString(pdf[:20]), "sha256": sum(pdf)}, "didn't arrive intact"},
		{map[string]any{"name": "Guide.pdf", "content_base64": "%%%"}, "isn't valid base64"},
		{map[string]any{"name": "Guide.pdf"}, "give the file's content"},
		{map[string]any{"name": "Guide.pdf", "text": "a", "content_base64": "YQ"}, "not both"},
		{map[string]any{"name": "Big.txt", "text": strings.Repeat("x", maxInlineBook+1)}, "at most 1 MiB"},
		{map[string]any{"name": "Big.pdf", "content_base64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, maxInlineBook+1))}, "add_book_from_url"},
		{map[string]any{"name": "Notes", "text": "a"}, "This file type can't be uploaded."},
		{map[string]any{"name": "Notes.md", "text": "a", "shelf": "Nowhere"}, "The shelves are: Work / Papers; Fiction"},
	} {
		if msg := refuse(t, s, "upload_book", c.args); !strings.Contains(msg, c.want) {
			t.Fatal(c.want, "≠", msg)
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_mcp_change`).Scan(&n); err != nil || n != 4 || len(books) != 0 {
		t.Fatal("refusals added or recorded", n, books, err)
	}
}

func TestAddBookFromURL(t *testing.T) {
	var books []string
	pdf := "%PDF-1.4\nsynthetic\n%%EOF\n"
	db, s := setup(t, options{booxLibrary: fakeBOOX{books: &books}, fetch: fileHost(map[string]string{
		"/papers/Field Guide.pdf": pdf, "/download": "disposition:Named.pdf\n" + pdf, "/raw": pdf})})
	var out addBookOutput
	text := call(t, s, "add_book_from_url", map[string]any{"url": "https://files.example/papers/Field%20Guide.pdf", "shelf": "s1"}, &out)
	if out.Name != "Field Guide.pdf" || out.Shelf != "Work / Papers" || out.Bytes != len(pdf) || len(books) != 1 || books[0] != "s1|Field Guide.pdf|"+pdf {
		t.Fatal(text, out, books)
	}
	call(t, s, "add_book_from_url", map[string]any{"url": "https://files.example/download?id=7"}, &out)
	if out.Name != "Named.pdf" {
		t.Fatal(out)
	}
	call(t, s, "add_book_from_url", map[string]any{"url": "https://files.example/raw", "name": "Chosen.pdf"}, &out)
	if out.Name != "Chosen.pdf" {
		t.Fatal(out)
	}
	var args string
	if err := db.QueryRowContext(ctx, `SELECT arguments FROM alexandria_mcp_change ORDER BY id LIMIT 1`).Scan(&args); err != nil || !strings.Contains(args, "files.example/papers") {
		t.Fatal(args, err)
	}
	for _, c := range []struct{ url, want string }{
		{"https://files.example/raw", "Give name with its extension"},
		{"https://files.example/missing.pdf", "answered 404"},
		{"http://files.example/raw", "plain https://"},
		{"https://192.168.1.10/a.pdf", "plain https://"},
		{"https://user:pw@files.example/a.pdf", "plain https://"},
	} {
		if msg := refuse(t, s, "add_book_from_url", map[string]any{"url": c.url}); !strings.Contains(msg, c.want) {
			t.Fatal(c.url, "≠", msg)
		}
	}
	if len(books) != 3 {
		t.Fatal(books)
	}
}

// The two tools share one day's allowance.
func TestBooksAreCappedTogether(t *testing.T) {
	db, s := setup(t, options{booxLibrary: fakeBOOX{}, fetch: fileHost(map[string]string{"/a.txt": "a"})})
	for i := range dailyBooksCap - 1 {
		call(t, s, "upload_book", map[string]any{"name": fmt.Sprint(i, ".txt"), "text": "a"}, nil)
	}
	call(t, s, "add_book_from_url", map[string]any{"url": "https://files.example/a.txt"}, nil)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{{"upload_book", map[string]any{"name": "more.txt", "text": "a"}}, {"add_book_from_url", map[string]any{"url": "https://files.example/a.txt"}}} {
		if msg := refuse(t, s, c.tool, c.args); !strings.Contains(msg, "add_book_from_url, request_book_upload and upload_book are together limited to 20 a day") {
			t.Fatal(msg)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE alexandria_mcp_change SET at=at-interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	call(t, s, "upload_book", map[string]any{"name": "tomorrow.txt", "text": "a"}, nil)
}

// send puts a body to an upload address and returns the status and reply.
func send(d Deps, method, address, body string, headers ...string) (int, map[string]any) {
	r := httptest.NewRequest(method, strings.TrimPrefix(address, "https://library.example"), strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	UploadHandler(d).ServeHTTP(w, r)
	var reply map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	return w.Code, reply
}

func TestUploadLinkTakesOneFile(t *testing.T) {
	var books []string
	var d Deps
	db, s := setup(t, options{booxLibrary: fakeBOOX{books: &books}, deps: &d})
	var link uploadLinkOutput
	text := call(t, s, "request_book_upload", map[string]any{"name": "Field Guide.pdf", "shelf": "Papers"}, &link)
	if !strings.HasPrefix(link.UploadURL, "https://library.example/upload/book/") || !OwnsUpload(strings.TrimPrefix(link.UploadURL, "https://library.example")) ||
		link.Method != "PUT" || link.Shelf != "Work / Papers" || link.Example != "curl -T 'Field Guide.pdf' '"+link.UploadURL+"'" || !strings.Contains(text, "curl -T") {
		t.Fatal(text, link)
	}
	pdf := "%PDF-1.4\nsynthetic\n%%EOF\n"
	for _, c := range []struct {
		method, address, body string
		headers               []string
		code                  int
		want                  string
	}{
		{"GET", link.UploadURL, "", nil, 405, "with PUT"},
		{"PUT", link.UploadURL, pdf, []string{"Content-Type", "multipart/form-data; boundary=x"}, 415, "curl -T"},
		{"PUT", link.UploadURL[:len(link.UploadURL)-2] + "AA", pdf, nil, 403, "isn't valid"},
		{"PUT", "https://library.example/upload/book/" + d.seal(uploadTicket{ID: "old", Name: "Old.pdf", Expires: time.Now().Add(-time.Second).Unix()}), pdf, nil, 403, "run out"},
		{"PUT", "https://library.example/upload/book/" + (Deps{UploadSecret: "another secret"}).seal(uploadTicket{ID: "x", Name: "X.pdf", Expires: time.Now().Add(time.Hour).Unix()}), pdf, nil, 403, "isn't valid"},
	} {
		if code, reply := send(d, c.method, c.address, c.body, c.headers...); code != c.code || !strings.Contains(fmt.Sprint(reply["error"]), c.want) {
			t.Fatal(c.method, c.code, c.want, "≠", code, reply)
		}
	}
	if len(books) != 0 {
		t.Fatal("a refused request added a book", books)
	}
	for range 2 { // the same file again is a retry
		code, reply := send(d, "PUT", link.UploadURL, pdf)
		book, _ := reply["book"].(map[string]any)
		if code != 201 || book["name"] != "Field Guide.pdf" || book["shelf"] != "Work / Papers" || book["sha256"] != sum([]byte(pdf)) || !strings.Contains(fmt.Sprint(reply["message"]), "Queued Field Guide.pdf") {
			t.Fatal(code, reply)
		}
	}
	if code, reply := send(d, "POST", link.UploadURL, pdf+"different"); code != 409 || !strings.Contains(fmt.Sprint(reply["error"]), "already taken a file") {
		t.Fatal(code, reply)
	}
	if len(books) != 2 || books[0] != "s1|Field Guide.pdf|"+pdf {
		t.Fatal(books)
	}
	// The record names the app that asked, once, and holds no content.
	var client, args, result string
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) OVER (),client,arguments,result FROM alexandria_mcp_change WHERE tool='upload_by_link'`).Scan(&n, &client, &args, &result); err != nil ||
		n != 1 || client != "Test app" || strings.Contains(args, "synthetic") || !strings.Contains(args, `"bytes":25`) || !strings.Contains(args, `"shelf":"Work / Papers"`) || !strings.HasPrefix(result, "Queued Field Guide.pdf") {
		t.Fatal(n, client, args, result, err)
	}

	// What the library refuses is said, and leaves the address usable.
	call(t, s, "request_book_upload", map[string]any{"name": "Notes.md"}, &link)
	bad := d
	bad.BOOX = refusingBOOX{}
	if code, reply := send(bad, "PUT", link.UploadURL, "x"); code != 422 || !strings.Contains(fmt.Sprint(reply["error"]), "Nothing was added. This file is already") {
		t.Fatal(code, reply)
	}
	if code, _ := send(d, "PUT", link.UploadURL, "# Notes"); code != 201 {
		t.Fatal(code)
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "Notes"}, "with its extension"},
		{map[string]any{"name": "a/b.pdf"}, "with its extension"},
		{map[string]any{"name": "Notes.md", "shelf": "Nowhere"}, "The shelves are: Work / Papers; Fiction"},
	} {
		if msg := refuse(t, s, "request_book_upload", c.args); !strings.Contains(msg, c.want) {
			t.Fatal(c.want, "≠", msg)
		}
	}
	// No secret or no public address: no such tool.
	for _, without := range []Deps{{BOOX: fakeBOOX{}, PublicURL: "https://library.example"}, {BOOX: fakeBOOX{}, UploadSecret: "s"}} {
		if registered := toolsOf(t, without); slices.Contains(registered, "request_book_upload") || !slices.Contains(registered, "upload_book") {
			t.Fatal(registered)
		}
	}
}

type refusingBOOX struct{ fakeBOOX }

func (refusingBOOX) AddBook(_ context.Context, _, _ string, _ []byte) (boox.BookAdded, error) {
	return boox.BookAdded{}, boox.BookRefused("This file is already in the library under another name.")
}

// toolsOf names the tools a server built from d offers.
func toolsOf(t *testing.T, d Deps) []string {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(d).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	names, _ := toolNames(t, session)
	return names
}
