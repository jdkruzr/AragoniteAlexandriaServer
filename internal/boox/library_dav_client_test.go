package boox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A real WebDAV client against the file view. Runs only where rclone is installed.
func TestLibraryDAVWithRclone(t *testing.T) {
	rclone, e := exec.LookPath("rclone")
	if e != nil {
		t.Skip("rclone not installed")
	}
	s, g := libraryFixture(t)
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryRecord(s, g, 3, "A", map[string]any{"name": "Alpha"})
	libraryRecord(s, g, 5, "m1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	managementObserve(t, s)
	server := httptest.NewServer(http.HandlerFunc(s.DAV))
	t.Cleanup(server.Close)
	remote := ":webdav,url='" + server.URL + DAVPrefix + "/',vendor=other:"
	dir := t.TempDir()
	run := func(ok bool, args ...string) string {
		t.Helper()
		cmd := exec.Command(rclone, append([]string{"--config", filepath.Join(dir, "rclone.conf"), "--retries", "1", "--low-level-retries", "1"}, args...)...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if (err == nil) != ok {
			t.Fatalf("rclone %v: %v\n%s%s", args, err, out, stderr.String())
		}
		return string(out)
	}
	names := func(p string) []string {
		t.Helper()
		var entries []struct{ Name string }
		if e := json.Unmarshal([]byte(run(true, "lsjson", remote+p)), &entries); e != nil {
			t.Fatal(e)
		}
		var out []string
		for _, x := range entries {
			out = append(out, x.Name)
		}
		return out
	}
	has := func(list []string, name string) bool {
		for _, n := range list {
			if n == name {
				return true
			}
		}
		return false
	}
	local := filepath.Join(dir, "Fresh.pdf")
	if e = os.WriteFile(local, []byte(syntheticPDF("rclone")), 0o600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain"), 0o600); e != nil {
		t.Fatal(e)
	}
	if root := names(""); !has(root, ".Trash") || !has(root, "Alpha") {
		t.Fatal(root)
	}
	run(true, "copyto", local, remote+"Alpha/Fresh.pdf")
	run(false, "copyto", filepath.Join(dir, "notes.txt"), remote+"Alpha/notes.txt")
	run(true, "mkdir", remote+"Alpha/New")
	run(true, "moveto", remote+"Alpha/Fresh.pdf", remote+"Alpha/New/Fresh.pdf")
	if got := run(true, "cat", remote+"Alpha/New/Fresh.pdf"); got != syntheticPDF("rclone") {
		t.Fatal(got)
	}
	run(true, "moveto", remote+"Alpha/One.epub", remote+"Alpha/One renamed.epub")
	run(true, "deletefile", remote+"Alpha/New/Fresh.pdf")
	if trash := names(".Trash"); !has(trash, "Fresh.pdf") {
		t.Fatal(trash)
	}
	run(true, "rmdir", remote+"Alpha/New")
	if alpha := names("Alpha"); !has(alpha, "One renamed.epub") || has(alpha, "New") || has(alpha, "notes.txt") {
		t.Fatal(alpha)
	}
	libraryDrain(t, s)
	if got := libraryStates(t, s); len(got) != 1 || got["published"] == 0 {
		t.Fatal(got)
	}
	if b := g.record(s, "b1"); b["name"] != "One renamed.epub" {
		t.Fatal(b)
	}
	if strings.Contains(run(true, "lsf", remote+".Trash"), "One") {
		t.Fatal("rename trashed the book")
	}
}
