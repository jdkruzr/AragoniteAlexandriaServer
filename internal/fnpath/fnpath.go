// Package fnpath defines the opaque ForestNote URI scheme used by Loom.
// Selectively ported from UltraBridge under Apache-2.0.
package fnpath

import "strings"

// Scheme is the prefix every ForestNote URI carries.
const Scheme = "forestnote://"

func Notebook(notebookID string) string { return Scheme + notebookID }

func Page(notebookID, pageID string) string { return Scheme + notebookID + "/" + pageID }

func Is(path string) bool { return strings.HasPrefix(path, Scheme) }

// PageID extracts the trailing segment. For a notebook URI it returns the
// notebook ID; page renderers are expected to pass a page URI.
func PageID(path string) string { return path[strings.LastIndex(path, "/")+1:] }

func NotebookID(path string) string {
	if !Is(path) {
		return ""
	}
	rest := strings.TrimPrefix(path, Scheme)
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}
