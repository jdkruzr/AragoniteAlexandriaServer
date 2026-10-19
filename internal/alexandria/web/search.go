package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

func (d Deps) searchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		q := strings.TrimSpace(values.Get("q"))
		mode, scope := values.Get("mode"), values.Get("source")
		if len(q) > 256 || (scope != "" && scope != "client" && scope != "boox") || (scope == "boox" && !d.Status.NativeBOOX) {
			http.Error(w, "Invalid search query or source.", 400)
			return
		}
		if mode != "keyword" {
			mode = ""
		}
		order := searchorder.Parse(values.Get("sort"))
		// Only normalized search state goes into navigation; submitting the search
		// form deliberately resets every result group's page.
		navigation := url.Values{"q": {q}, "mode": {mode}, "source": {scope}, "sort": {string(order)}}
		for _, key := range []string{"pages_offset", "titles_offset", "annotations_offset"} {
			navigation.Set(key, strconv.Itoa(searchorder.Offset(values.Get(key))))
		}
		data := map[string]any{"Query": q, "Mode": mode, "Source": scope, "Sort": string(order), "Semantic": d.Status.Embedding != "", "BOOX": d.Status.NativeBOOX, "DateSort": order == "created_asc" || order == "created_desc"}
		if q != "" {
			if d.Status.NativeBOOX && scope != "client" {
				offset := searchorder.Offset(values.Get("titles_offset"))
				matches, total, err := d.searchBOOX(r.Context(), q, order, offset)
				if err == nil && offset > 0 && len(matches) == 0 {
					offset = 0
					matches, total, err = d.searchBOOX(r.Context(), q, order, offset)
				}
				if err != nil {
					d.fail(w, err)
					return
				}
				data["Native"], data["TitlesPage"] = matches, searchorder.Navigation(navigation, "titles_offset", "titles-results", offset, total)
			}
			searchScope := scope
			if !d.Status.NativeBOOX {
				searchScope = "client"
			}
			offset := searchorder.Offset(values.Get("pages_offset"))
			pages, total, err := d.Search.SearchPage(r.Context(), q, searchorder.Size, offset, mode != "keyword", searchScope, order)
			if offset >= total {
				offset = 0
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Results"], data["PagesPage"] = pages, searchorder.Navigation(navigation, "pages_offset", "pages-results", offset, total)
			if scope != "boox" && len(strings.Fields(q)) <= 16 {
				offset := searchorder.Offset(values.Get("annotations_offset"))
				store := readersearch.New(d.DB)
				annotations, total, err := store.SearchPage(r.Context(), q, "", searchorder.Size, offset, order)
				if err == nil && offset > 0 && len(annotations) == 0 {
					offset = 0
					annotations, total, err = store.SearchPage(r.Context(), q, "", searchorder.Size, offset, order)
				}
				if err != nil {
					d.fail(w, err)
					return
				}
				data["Annotations"], data["AnnotationsPage"] = annotations, searchorder.Navigation(navigation, "annotations_offset", "annotations-results", offset, total)
			}
		}
		d.render(w, r, "search", "Search", "search", data)
	})
}
