package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
)

// Phosphor duotone icons (MIT, @phosphor-icons/core 2.1.1; see icons/LICENSE),
// inlined so they work under every page's content security policy.
//
//go:embed icons/*.svg
var iconFS embed.FS

var icons = func() map[string]string {
	names, err := fs.Glob(iconFS, "icons/*.svg")
	if err != nil {
		panic(err)
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		b, err := iconFS.ReadFile(name)
		if err != nil {
			panic(err)
		}
		svg := strings.TrimSpace(string(b))
		if !strings.HasPrefix(svg, "<svg ") {
			panic("icon " + name + " is not an SVG element")
		}
		out[strings.TrimSuffix(strings.TrimPrefix(name, "icons/"), ".svg")] = svg[len("<svg "):]
	}
	return out
}()

// icon renders a decorative icon sized to the surrounding text; any label
// belongs to the link or button around it. An optional class is added.
func icon(name string, class ...string) (template.HTML, error) {
	svg, ok := icons[name]
	if !ok {
		return "", fmt.Errorf("unknown icon %q", name)
	}
	c := "icon"
	if len(class) == 1 && class[0] != "" {
		c += " " + template.HTMLEscapeString(class[0])
	}
	return template.HTML(`<svg class="` + c + `" aria-hidden="true" focusable="false" width="1em" height="1em" ` + svg), nil
}
