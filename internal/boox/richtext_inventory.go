package boox

import (
	"context"
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// RichTextInventory counts the structure of text notebooks' editor resources
// (node types, marks, attributes; HTML tags) without reading out their text,
// so renderer coverage can be checked against real notes privately.
type RichTextInventory struct {
	Notebooks, Resources, JSON, HTML, Unreadable int
	Nodes                                        map[string]int // node type → count
	Marks                                        map[string]int // mark type → count
	Attrs                                        map[string]int // "node.attr=value" (values only for small enums) → count
	Tags                                         map[string]int // HTML tag → count
	TopKeys                                      map[string]int // envelope keys
}

func (s Service) RichTextInventory(ctx context.Context) (RichTextInventory, error) {
	inv := RichTextInventory{Nodes: map[string]int{}, Marks: map[string]int{}, Attrs: map[string]int{}, Tags: map[string]int{}, TopKeys: map[string]int{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT document_id FROM boox_search_notebooks WHERE body->>'activeScene'='1' ORDER BY document_id`)
	if err != nil {
		return inv, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return inv, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		n, err := s.notebookSnapshot(ctx, id)
		if err != nil {
			inv.Unreadable++
			continue
		}
		inv.Notebooks++
		load := s.snapshotLoader(ctx, n)
		for _, k := range n.Keys {
			if !strings.HasPrefix(k, "resource/data/") || !strings.HasSuffix(k, ".html") || n.Assets[k].Bytes > 1<<20 {
				continue
			}
			b, err := load(k)
			if err != nil {
				inv.Unreadable++
				continue
			}
			inv.Resources++
			var env map[string]json.RawMessage
			if json.Unmarshal(b, &env) == nil && env["content"] != nil {
				inv.JSON++
				for key := range env {
					inv.TopKeys[key]++
				}
				var root any
				_ = json.Unmarshal(env["content"], &root)
				inv.walk(root, 0)
				continue
			}
			inv.HTML++
			t := html.NewTokenizer(strings.NewReader(string(b)))
			for tt := t.Next(); tt != html.ErrorToken; tt = t.Next() {
				if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
					name, _ := t.TagName()
					inv.Tags[string(name)]++
				}
			}
		}
	}
	return inv, nil
}

func (inv *RichTextInventory) walk(v any, depth int) {
	node, ok := v.(map[string]any)
	if !ok || depth > 64 {
		return
	}
	typ, _ := node["type"].(string)
	inv.Nodes[typ]++
	if attrs, ok := node["attrs"].(map[string]any); ok {
		for k, a := range attrs {
			switch x := a.(type) {
			case string:
				if len(x) <= 16 && !strings.ContainsAny(x, " /") {
					inv.Attrs[typ+"."+k+"="+x]++
				} else {
					inv.Attrs[typ+"."+k+"=<string>"]++
				}
			case float64, bool, nil:
				b, _ := json.Marshal(x)
				inv.Attrs[typ+"."+k+"="+string(b)]++
			default:
				inv.Attrs[typ+"."+k+"=<object>"]++
			}
		}
	}
	if marks, ok := node["marks"].([]any); ok {
		for _, m := range marks {
			if mm, ok := m.(map[string]any); ok {
				mt, _ := mm["type"].(string)
				inv.Marks[mt]++
				if attrs, ok := mm["attrs"].(map[string]any); ok {
					for k, a := range attrs {
						b, _ := json.Marshal(a)
						if len(b) > 20 {
							b = []byte("<long>")
						}
						inv.Attrs["mark:"+mt+"."+k+"="+string(b)]++
					}
				}
			}
		}
	}
	if kids, ok := node["content"].([]any); ok {
		for _, c := range kids {
			inv.walk(c, depth+1)
		}
	}
}
