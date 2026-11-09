package searchview

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
)

// Folder narrows a search to the notebooks in one folder, its folders
// included. Source is "boox" or "client".
type Folder struct{ Source, ID string }

// FolderChoice is a folder a search can be narrowed to, with its path from
// the top ("Work / 2026").
type FolderChoice struct{ Source, ID, Path string }

// booxFolder is a live BOOX folder record, as the library browser counts one.
const booxFolder = `f.domain='notebook' AND f.body->>'type'='0' AND f.body->>'uniqueId'=f.document_id
 AND NOT f.body ? 'commitType' AND coalesce(f.body->>'status','1')='1'`

// Folders lists the folders a search can be narrowed to: BOOX ones (when
// the library has BOOX) and then Client ones, each by path.
func (s Searcher) Folders(ctx context.Context, nativeBOOX bool) ([]FolderChoice, error) {
	type folder struct{ id, name, parent string }
	var out []FolderChoice
	for _, source := range []string{"boox", "client"} {
		if source == "boox" && !nativeBOOX {
			continue
		}
		q := `SELECT f.document_id,coalesce(f.body->>'title',''),coalesce(f.body->>'parentUniqueId','') FROM boox_projection f WHERE ` + booxFolder
		if source == "client" {
			q = `SELECT id,coalesce(name,''),coalesce(parent_folder_id,'') FROM fn_folder WHERE deleted_at IS NULL`
		}
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		byID := map[string]folder{}
		for rows.Next() {
			var f folder
			if err = rows.Scan(&f.id, &f.name, &f.parent); err != nil {
				rows.Close()
				return nil, err
			}
			if strings.TrimSpace(f.name) == "" {
				f.name = "Untitled"
			}
			byID[f.id] = f
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		var list []FolderChoice
		for _, f := range byID {
			parts := []string{f.name}
			// A missing parent ends the path; a loop can't run forever.
			for p, depth := byID[f.parent], 0; p.id != "" && depth < 64; p, depth = byID[p.parent], depth+1 {
				parts = append([]string{p.name}, parts...)
			}
			list = append(list, FolderChoice{Source: source, ID: f.id, Path: strings.Join(parts, " / ")})
		}
		slices.SortFunc(list, func(a, b FolderChoice) int {
			if c := cmp.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path)); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
		out = append(out, list...)
	}
	return out, nil
}

// folderNotebooks lists the live notebooks in a folder and its folders:
// BOOX document IDs or Client notebook IDs. A folder that doesn't exist
// holds none.
func (s Searcher) folderNotebooks(ctx context.Context, f Folder) ([]string, error) {
	q := `WITH RECURSIVE tree(id,depth) AS (
  SELECT f.document_id,0 FROM boox_projection f WHERE f.document_id=$1 AND ` + booxFolder + `
  UNION SELECT f.document_id,tree.depth+1 FROM boox_projection f JOIN tree ON f.body->>'parentUniqueId'=tree.id WHERE ` + booxFolder + ` AND tree.depth<64)
 SELECT DISTINCT p.document_id FROM boox_projection p JOIN tree ON p.body->>'parentUniqueId'=tree.id WHERE ` + booxvisibility.Notebook("p")
	if f.Source == "client" {
		q = `WITH RECURSIVE tree(id,depth) AS (
  SELECT id,0 FROM fn_folder WHERE id=$1 AND deleted_at IS NULL
  UNION SELECT c.id,tree.depth+1 FROM fn_folder c JOIN tree ON c.parent_folder_id=tree.id WHERE c.deleted_at IS NULL AND tree.depth<64)
 SELECT DISTINCT n.id FROM fn_notebook n JOIN tree ON n.folder_id=tree.id WHERE n.deleted_at IS NULL`
	}
	rows, err := s.DB.QueryContext(ctx, q, f.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
