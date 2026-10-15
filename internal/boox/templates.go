package boox

import (
	"embed"
	"html/template"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// templateFS holds the BOOX views and the text written onto tablets, kept out
// of Go source so their wording can be translated and reviewed as files.
//
//go:embed templates
var templateFS embed.FS

// viewFuncs are the site's template functions, so BOOX views resolve the
// same names (including t) as the core pages they're grafted into.
func viewFuncs() template.FuncMap { return web.Funcs() }

// listFuncs are the site's template functions with "ms" as a list date.
func listFuncs() template.FuncMap {
	f := template.FuncMap{}
	for k, v := range web.Funcs() {
		f[k] = v
	}
	f["ms"] = listview.Date
	return f
}
