package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Availability struct {
	Label, Detail string
	Ready         bool
}

// EmbeddingAvailability checks installed model metadata only. Settings page
// views never load a model, run inference, download weights or send note text.
func EmbeddingAvailability(ctx context.Context, c EmbedConfig) Availability {
	if c.URL == "" || c.Model == "" {
		return Availability{Label: "Not configured", Detail: "Choose a provider in Advanced settings. The optional local Ollama service must be installed before it can be used."}
	}
	unavailable := Availability{Label: "Unavailable", Detail: "Alexandria could not check the saved provider. Check its address and service, then refresh."}
	if endpoint(c.URL) != nil {
		return unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.URL, "/")+"/api/tags", nil)
	if err != nil {
		return unavailable
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return unavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return unavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return unavailable
	}
	var list struct {
		Models []struct{ Name, Model string }
	}
	if json.Unmarshal(raw, &list) != nil || list.Models == nil {
		return unavailable
	}
	normalize := func(name string) string {
		if name != "" && !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
			return name + ":latest"
		}
		return name
	}
	for _, m := range list.Models {
		if normalize(m.Name) == normalize(c.Model) || normalize(m.Model) == normalize(c.Model) {
			return Availability{Label: "Ready", Detail: "The saved Ollama provider is reachable and this model is installed. Availability was checked without running inference.", Ready: true}
		}
	}
	return Availability{Label: "Model missing", Detail: "Ollama is reachable, but the saved model is not installed. Install it on that service or choose an installed embedding model."}
}
