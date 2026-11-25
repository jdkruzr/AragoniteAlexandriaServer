package mcptools

// A one-time upload address, for a file too large to send in a call and not
// on the public web. The assistant asks for the address over MCP, where it
// is known by its token, and whoever holds the address then sends the file
// to it with no sign-in: the address is the permission, for one named file
// on one shelf, for a quarter of an hour.
//
// The address is signed rather than stored. That it works once is kept by
// the record of changes: a second file sent to a used address is refused,
// and the same file again is a retry. (Two different files sent at the same
// instant could both land; each is still only a new book.)

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
)

// UploadPrefix is where one-time book uploads are sent.
const UploadPrefix = "/upload/book/"

// OwnsUpload reports a one-time upload address.
func OwnsUpload(path string) bool { return strings.HasPrefix(path, UploadPrefix) }

const (
	uploadLinkLife = 15 * time.Minute
	// uploadByLink is the record's name for a file sent to an address.
	uploadByLink = "upload_by_link"
)

// uploadTicket is what an upload address permits.
type uploadTicket struct {
	ID      string `json:"i"`
	Name    string `json:"n"`
	Shelf   string `json:"s,omitempty"` // shelf ID
	Expires int64  `json:"e"`           // Unix seconds
	Client  string `json:"c,omitempty"` // the app that asked
}

func (d Deps) signTicket(payload string) string {
	mac := hmac.New(sha256.New, []byte(d.UploadSecret))
	mac.Write([]byte("book-upload\n" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// seal makes the address's last part from a ticket.
func (d Deps) seal(t uploadTicket) string {
	raw, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + d.signTicket(payload)
}

// open reads a ticket this server signed and that hasn't run out.
func (d Deps) open(sealed string, now time.Time) (uploadTicket, bool) {
	var t uploadTicket
	payload, sig, ok := strings.Cut(sealed, ".")
	if !ok || d.UploadSecret == "" || !hmac.Equal([]byte(sig), []byte(d.signTicket(payload))) {
		return t, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(raw, &t) != nil || t.ID == "" || now.Unix() > t.Expires {
		return uploadTicket{}, false
	}
	return t, true
}

type uploadLinkInput struct {
	Name  string `json:"name" jsonschema:"the file's name with its extension, such as Field Guide.pdf"`
	Shelf string `json:"shelf,omitempty" jsonschema:"a shelf's path such as Work / Papers, or its name; default no shelf"`
}

type uploadLinkOutput struct {
	UploadURL string `json:"upload_url" jsonschema:"send the file's bytes here with PUT; works once, no sign-in"`
	Method    string `json:"method"`
	Example   string `json:"example" jsonschema:"a command that sends the file"`
	Expires   string `json:"expires"`
	Name      string `json:"name"`
	Shelf     string `json:"shelf,omitempty"`
}

// sentByLink is the record of a file sent to an address.
type sentByLink struct {
	Ticket string `json:"ticket"`
	sentBook
}

func registerUploadLink(s *mcp.Server, d Deps) {
	if d.UploadSecret == "" || d.PublicURL == "" {
		return
	}
	add(s, d, tool{name: "request_book_upload", title: "Get a one-time address to upload a book", write: true,
		description: "Get an address to send a book or document's bytes to, for a file up to 128 MiB that you hold and that isn't on the public " +
			"web. Send the file to upload_url with an HTTP PUT whose body is the file (for example curl -T). The address needs no sign-in, " +
			"takes one file, and stops working after 15 minutes. It only helps if you can make requests to this server from where the file " +
			"is; if you can't, say so rather than retrying, and use add_book_from_url or upload_book instead. The book is added only once " +
			"the file arrives. At most 20 books a day."},
		func(ctx context.Context, in uploadLinkInput) (string, uploadLinkOutput, error) {
			name := strings.TrimSpace(in.Name)
			if name == "" || !strings.Contains(name, ".") || strings.ContainsAny(name, "/\\") {
				return "", uploadLinkOutput{}, errors.New("give the file's name with its extension, such as Field Guide.pdf")
			}
			shelf, err := d.BOOX.ResolveShelf(ctx, in.Shelf)
			if err != nil {
				return "", uploadLinkOutput{}, d.withShelves(ctx, err)
			}
			until := time.Now().Add(uploadLinkLife)
			address := d.link(UploadPrefix + d.seal(uploadTicket{ID: uuid.NewString(), Name: name, Shelf: shelf.ID, Expires: until.Unix(), Client: d.Client}))
			out := uploadLinkOutput{UploadURL: address, Method: "PUT", Example: "curl -T '" + strings.ReplaceAll(name, "'", `'\''`) + "' '" + address + "'",
				Expires: when(until, d.zone(ctx)), Name: name, Shelf: shelf.Path}
			return fmt.Sprintf("Made a one-time upload address for %s, good until %s.\nSend the file with: %s\n", name, out.Expires, out.Example), out, nil
		})
}

// UploadHandler takes a file sent to a one-time address. It is public by
// design: the signed address is the only guard, as the tool that made it is
// the only way to get one.
func UploadHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := func(code int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(v)
		}
		refuse := func(code int, why string) { answer(code, map[string]string{"error": why}) }
		ticket, ok := d.open(strings.TrimPrefix(r.URL.Path, UploadPrefix), time.Now())
		if !ok || d.BOOX == nil {
			refuse(http.StatusForbidden, "This upload address isn't valid or has run out. Ask for a new one with request_book_upload.")
			return
		}
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			w.Header().Set("Allow", "PUT, POST")
			refuse(http.StatusMethodNotAllowed, "Send the file's bytes with PUT.")
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			refuse(http.StatusUnsupportedMediaType, "Send the file's bytes as the request body, not as a form (curl -T, not curl -F).")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFetchedBook))
		if err != nil {
			refuse(http.StatusRequestEntityTooLarge, "The file couldn't be read, or is larger than the 128 MiB a book can be.")
			return
		}
		ctx := r.Context()
		sent := sentByLink{Ticket: ticket.ID, sentBook: sentBook{Name: ticket.Name, Bytes: len(raw), SHA256: sum(raw)}}
		var before string
		err = d.DB.QueryRowContext(ctx, `SELECT arguments FROM alexandria_mcp_change WHERE tool=$1 AND arguments LIKE $2 LIMIT 1`,
			uploadByLink, `{"ticket":"`+ticket.ID+`"%`).Scan(&before)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			refuse(http.StatusServiceUnavailable, "The library is temporarily unavailable.")
			return
		}
		if err == nil && !strings.Contains(before, `"sha256":"`+sent.SHA256+`"`) {
			refuse(http.StatusConflict, "This upload address has already taken a file. Ask for a new one with request_book_upload.")
			return
		}
		retry := err == nil
		added, err := d.BOOX.AddBook(ctx, ticket.Shelf, ticket.Name, raw)
		var refused boox.BookRefused
		switch {
		case errors.As(err, &refused), errors.Is(err, boox.ErrNoShelf):
			refuse(http.StatusUnprocessableEntity, "Nothing was added. "+err.Error())
			return
		case err != nil:
			refuse(http.StatusServiceUnavailable, "The library is temporarily unavailable.")
			return
		}
		sent.Shelf = added.Shelf
		text := fmt.Sprintf("Queued %s (%s). It reaches the tablets on their next sync.", added.Name, size(len(raw)))
		out := addBookOutput{Book: ref(sourceBOOX, added.ID), Name: added.Name, Shelf: added.Shelf, Bytes: len(raw), SHA256: sent.SHA256, State: "queued",
			URL: d.link("/boox/reading?selected=" + url.QueryEscape(added.ID))}
		if !retry {
			if err := record(ctx, d.DB, ticket.Client, uploadByLink, sent, text); err != nil {
				// Unrecorded, the address would take another file; the book stands.
				answer(http.StatusCreated, map[string]any{"book": out, "message": text, "warning": "This upload wasn't added to the owner's record of assistant changes."})
				return
			}
		}
		answer(http.StatusCreated, map[string]any{"book": out, "message": text})
	})
}
