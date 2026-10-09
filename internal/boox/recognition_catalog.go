package boox

import (
	"context"
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

func (s Service) RecognitionNotebook(ctx context.Context, id string) (recognition.Notebook, error) {
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return recognition.Notebook{}, e
	}
	if n.Meta.Status != 1 || n.IndexWarning != "" {
		return recognition.Notebook{}, errors.New("Notebook is deleted or its page catalog is incomplete")
	}
	out := recognition.Notebook{ID: id, Title: n.Meta.Title}
	for i, p := range booxpage.PageIDs(n.Meta.PageNameList) {
		out.Pages = append(out.Pages, recognition.Page{ID: p, Number: i + 1})
	}
	return out, nil
}
