package boox

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The file view presents stored books as files and writable shelves as
// folders. A book appears in every visible shelf it belongs to, unshelved
// books at the top level, and removed stored books in a flat .Trash folder.
// Names are display-only: duplicates in one folder get " (2)"-style suffixes
// that are never written back.
const libraryTrashKey = "\x00trash"

type libraryEntry struct {
	Name, Folder, Key string // Folder: key of the containing folder; Key: a folder's own key
	Book              *libraryBook
	Placeholder       bool
}

func (e libraryEntry) Dir() bool { return e.Book == nil && !e.Placeholder }

type libraryIndex struct {
	*librarySnapshot
	children map[string][]libraryEntry
}

func displayName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '_'
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, name)
	if strings.TrimSpace(name) == "" || name == "." || name == ".." {
		return ""
	}
	return name
}
func bookFileName(b *libraryBook) string {
	if name := displayName(b.Name); name != "" {
		return name
	}
	ext := ""
	if b.Kind != "" {
		ext = "." + b.Kind
	}
	if title := displayName(b.Title); title != "" {
		return title + ext
	}
	return b.ID + ext
}

func newLibraryIndex(v *librarySnapshot) *libraryIndex {
	x := &libraryIndex{librarySnapshot: v, children: map[string][]libraryEntry{}}
	x.children[""] = []libraryEntry{{Name: libraryTrash, Key: libraryTrashKey}}
	for id, f := range v.Shelves {
		if v.Shelf(id) == nil {
			continue
		}
		name := displayName(f.Name)
		if name == "" {
			name = "Untitled shelf"
		}
		x.children[f.Parent] = append(x.children[f.Parent], libraryEntry{Name: name, Folder: f.Parent, Key: id})
	}
	for _, b := range v.Books {
		if !b.Live {
			x.children[libraryTrashKey] = append(x.children[libraryTrashKey], libraryEntry{Name: bookFileName(b), Folder: libraryTrashKey, Book: b})
			continue
		}
		members := v.Memberships(b.ID)
		if len(members) == 0 {
			x.children[""] = append(x.children[""], libraryEntry{Name: bookFileName(b), Book: b})
		}
		seen := map[string]bool{}
		for _, m := range members {
			if !seen[m.Shelf] {
				seen[m.Shelf] = true
				x.children[m.Shelf] = append(x.children[m.Shelf], libraryEntry{Name: bookFileName(b), Folder: m.Shelf, Book: b})
			}
		}
	}
	for key, list := range x.children {
		x.children[key] = disambiguate(list, key == "")
	}
	return x
}

func (e libraryEntry) sortID() string {
	if e.Book != nil {
		return e.Book.ID
	}
	return e.Key
}

// disambiguate gives each name to its first claimant in a stable order and
// suffixes the rest. Comparison ignores case, as Finder and Explorer do.
func disambiguate(list []libraryEntry, root bool) []libraryEntry {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if root && (a.Key == libraryTrashKey) != (b.Key == libraryTrashKey) {
			return a.Key == libraryTrashKey
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Dir() != b.Dir() {
			return a.Dir()
		}
		return a.sortID() < b.sortID()
	})
	used := map[string]bool{}
	var duplicates []int
	for i, e := range list {
		if used[strings.ToLower(e.Name)] {
			duplicates = append(duplicates, i)
			continue
		}
		used[strings.ToLower(e.Name)] = true
	}
	for _, i := range duplicates {
		base, ext := list[i].Name, ""
		if !list[i].Dir() {
			ext = path.Ext(base)
			base = strings.TrimSuffix(base, ext)
		}
		for n := 2; ; n++ {
			candidate := base + " (" + strconv.Itoa(n) + ")" + ext
			if !used[strings.ToLower(candidate)] {
				used[strings.ToLower(candidate)] = true
				list[i].Name = candidate
				break
			}
		}
	}
	return list
}

// Children lists a folder, including Explorer placeholders for it.
func (x *libraryIndex) Children(key string, placeholders []string) []libraryEntry {
	out := append([]libraryEntry(nil), x.children[key]...)
	for _, name := range placeholders {
		if _, found := x.find(out, name); !found {
			out = append(out, libraryEntry{Name: name, Folder: key, Placeholder: true})
		}
	}
	return out
}
func (x *libraryIndex) find(list []libraryEntry, name string) (libraryEntry, bool) {
	for _, e := range list {
		if e.Name == name {
			return e, true
		}
	}
	return libraryEntry{}, false
}

// Lookup resolves a cleaned slash path relative to the file view's root.
func (x *libraryIndex) Lookup(p string, placeholders func(key string) []string) (libraryEntry, bool) {
	at := libraryEntry{}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return at, true
	}
	if len(parts) > 34 {
		return libraryEntry{}, false
	}
	for _, part := range parts {
		if !at.Dir() {
			return libraryEntry{}, false
		}
		next, found := x.find(x.Children(at.Key, placeholders(at.Key)), part)
		if !found {
			return libraryEntry{}, false
		}
		at = next
	}
	return at, true
}
