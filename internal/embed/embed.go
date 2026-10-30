package embed

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ollama/ollama/api"
)

// Embedder é o gateway de embeddings (ollama / bge-m3)
type Embedder struct {
	client *api.Client
	model  string
}

// New cria o Embedder.
// baseURL ex: "http://localhost:11434" (ollama) / model ex: "bge-m3"
func New(baseURL, model string) (*Embedder, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	client := api.NewClient(u, http.DefaultClient)
	return &Embedder{client: client, model: model}, nil
}

// Embed gera o vetor (1024 floats no bge-m3) de um texto.
func (e *Embedder) Embed(ctx context.Context, texto string) ([]float32, error) {
	resp, err := e.client.Embed(ctx, &api.EmbedRequest{
		Model: e.model,
		Input: texto, // Input é 'any': uma string ou []string(varios textos)
	})
	if err != nil {
		return nil, fmt.Errorf("embedding no ollama: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("embed: ollama retornou 0 vetores")
	}
	return resp.Embeddings[0], nil
}
