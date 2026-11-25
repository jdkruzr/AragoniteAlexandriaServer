package mcptools

// Tools that add a book or document to the BOOX shelves. A file reaches the
// server one of two ways: the server downloads it from a public address, or
// the assistant sends its content in the call, which only suits text it
// wrote and small files, since an assistant writes every byte it sends.
// Both take the web upload's path: nothing existing is replaced.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
)

const (
	// maxInlineBook bounds a book sent in a call.
	maxInlineBook = 1 << 20
	// maxFetchedBook bounds a book downloaded by address (the upload limit).
	maxFetchedBook = 128 << 20
)

type addBookURLInput struct {
	URL   string `json:"url" jsonschema:"an https address that anyone can download the file from without signing in"`
	Name  string `json:"name,omitempty" jsonschema:"the file's name with its extension, such as Field Guide.pdf; default the name its address gives"`
	Shelf string `json:"shelf,omitempty" jsonschema:"a shelf's path such as Work / Papers, or its name; default no shelf"`
}

type uploadBookInput struct {
	Name    string `json:"name" jsonschema:"the file's name with its extension, such as Summary.md or Field Guide.pdf"`
	Text    string `json:"text,omitempty" jsonschema:"the file's content as written, for .md, .txt and .html files; give this or content_base64"`
	Content string `json:"content_base64,omitempty" jsonschema:"the file's bytes in base64, for other small files; give this or text"`
	SHA256  string `json:"sha256,omitempty" jsonschema:"the file's SHA-256 in hex; when given, a file that arrived changed is refused"`
	Shelf   string `json:"shelf,omitempty" jsonschema:"a shelf's path such as Work / Papers, or its name; default no shelf"`
}

// bytes is the file sent: the text as written, or the decoded base64.
func (in uploadBookInput) bytes() ([]byte, error) {
	switch {
	case in.Text != "" && in.Content != "":
		return nil, errors.New("give text or content_base64, not both")
	case in.Text != "":
		return []byte(in.Text), nil
	case in.Content == "":
		return nil, errors.New("give the file's content, as text or content_base64")
	}
	// Assistants wrap lines and vary the alphabet and padding.
	packed := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t', '=':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, in.Content)
	if base64.RawStdEncoding.DecodedLen(len(packed)) > maxInlineBook {
		return nil, errTooLargeInline
	}
	raw, err := base64.RawStdEncoding.DecodeString(packed)
	if err != nil {
		return nil, errors.New("content_base64 isn't valid base64")
	}
	return raw, nil
}

var errTooLargeInline = errors.New("a file sent in a call can be at most 1 MiB; for a larger one use add_book_from_url, or the owner can upload it in the web app")

// recorded keeps what was sent, not the file itself.
func (in uploadBookInput) recorded() any {
	raw, _ := in.bytes()
	return sentBook{Name: in.Name, Shelf: in.Shelf, Bytes: len(raw), SHA256: sum(raw)}
}

type sentBook struct {
	Name   string `json:"name"`
	Shelf  string `json:"shelf,omitempty"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type addBookOutput struct {
	Book   string `json:"book" jsonschema:"reference for get_book, once the book is published"`
	Name   string `json:"name"`
	Shelf  string `json:"shelf,omitempty"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

func sum(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

// size is a byte count for reading.
func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// addBook files a book and words the result; an unknown shelf is answered
// with the shelves there are.
func (d Deps) addBook(ctx context.Context, shelf, name string, raw []byte) (string, addBookOutput, error) {
	added, err := d.BOOX.AddBook(ctx, shelf, name, raw)
	if err != nil {
		return "", addBookOutput{}, d.withShelves(ctx, err)
	}
	out := addBookOutput{Book: ref(sourceBOOX, added.ID), Name: added.Name, Shelf: added.Shelf, Bytes: len(raw), SHA256: sum(raw), State: "queued",
		URL: d.link("/boox/reading?selected=" + url.QueryEscape(added.ID))}
	where := "with no shelf"
	if added.Shelf != "" {
		where = "on the shelf " + added.Shelf
	}
	return fmt.Sprintf("Queued %s (%s) %s. It reaches the tablets on their next sync.\n", added.Name, size(len(raw)), where), out, nil
}

// withShelves adds the shelves there are to an unknown-shelf refusal.
func (d Deps) withShelves(ctx context.Context, err error) error {
	if !errors.Is(err, boox.ErrNoShelf) {
		return err
	}
	shelves, e := d.BOOX.Shelves(ctx)
	if e != nil {
		return err
	}
	if len(shelves) == 0 {
		return fmt.Errorf("%w There are no shelves yet; leave shelf out to add the book without one", err)
	}
	paths := make([]string, 0, len(shelves))
	for _, s := range shelves {
		paths = append(paths, s.Path)
	}
	return fmt.Errorf("%w The shelves are: %s", err, clip(strings.Join(paths, "; "), 1500))
}

// fetchProblems words the download failures.
var fetchProblems = map[string]string{
	"invalid_url":  "the address must be a plain https:// address on the public internet",
	"fetch_failed": "the file couldn't be downloaded from that address (it may be private, slow or unreachable from this server)",
	"too_large":    "the file is larger than the 128 MiB a book can be",
}

func registerUploads(s *mcp.Server, d Deps) {
	if d.BOOX == nil {
		return
	}
	add(s, d, tool{name: "add_book_from_url", title: "Add a book from an address", write: true, idempotent: true, openWorld: true,
		description: "Add a book or document to the owner's BOOX tablets by its https address: this server downloads the file and files it on a " +
			"shelf. Use this for any file on the public web, of any size up to 128 MiB; the address must work without signing in. " +
			"PDF, EPUB, MOBI, AZW3, CBZ, DJVU, FB2, DOCX, TXT, Markdown, HTML and the other formats the tablets read are accepted, by the " +
			"name's extension. It only ever adds a book; nothing existing is replaced. The book reaches the tablets on their next sync. " +
			"At most 20 books a day."},
		func(ctx context.Context, in addBookURLInput) (string, addBookOutput, error) {
			client := d.Fetch
			if client == nil {
				client = subscriptions.DownloadClient()
			}
			f, code := subscriptions.Download(ctx, client, strings.TrimSpace(in.URL), maxFetchedBook)
			if code != "" {
				problem, ok := fetchProblems[code]
				if !ok {
					problem = "the address answered " + strings.TrimPrefix(code, "http_") + " instead of the file"
				}
				return "", addBookOutput{}, errors.New("Nothing was added: " + problem + ".")
			}
			name := strings.TrimSpace(in.Name)
			if name == "" {
				name = f.Name
			}
			if !strings.Contains(name, ".") {
				return "", addBookOutput{}, errors.New("Nothing was added: the address doesn't say what kind of file this is. Give name with its extension, such as Field Guide.pdf.")
			}
			return d.addBook(ctx, in.Shelf, name, f.Raw)
		})

	add(s, d, tool{name: "upload_book", title: "Add a book from its content", write: true, idempotent: true,
		description: "Add a document to the owner's BOOX tablets from content sent in this call: text you wrote (give text, with a name ending " +
			".md, .txt or .html), or a small file's bytes (content_base64, at most 1 MiB decoded; add sha256 so a file that arrived changed is " +
			"refused). For anything larger or already on the web, use add_book_from_url instead. It only ever adds a book; nothing existing " +
			"is replaced. The book reaches the tablets on their next sync. At most 20 books a day."},
		func(ctx context.Context, in uploadBookInput) (string, addBookOutput, error) {
			raw, err := in.bytes()
			if err != nil {
				return "", addBookOutput{}, err
			}
			if len(raw) > maxInlineBook {
				return "", addBookOutput{}, errTooLargeInline
			}
			if want := strings.ToLower(strings.TrimSpace(in.SHA256)); want != "" && want != sum(raw) {
				return "", addBookOutput{}, fmt.Errorf("Nothing was added: the content didn't arrive intact (%s, SHA-256 %s, not the one given). Send it again.", size(len(raw)), sum(raw))
			}
			return d.addBook(ctx, in.Shelf, in.Name, raw)
		})
	registerUploadLink(s, d)
}
