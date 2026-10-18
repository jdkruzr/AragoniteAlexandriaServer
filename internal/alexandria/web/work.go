package web

import (
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/status"
)

type WorkSummary = status.WorkSummary
type WorkGroup = status.WorkGroup
type WorkRow = status.WorkRow

// BOOXWorkFunc reports WorkSummary; nil without BOOX.
type BOOXWorkFunc func(context.Context) (WorkSummary, error)
