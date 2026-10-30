package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Guilherme-AVVO/mini-rag/internal/rag"
)

type Handler struct {
	service *rag.Service
}

func New(service *rag.Service) *Handler {
	return &Handler{service: service}
}

// Routes devolve o Router chi com as rotas registradas
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/ask", h.ask)
	return r
}

type askRequest struct {
	Pergunta string `json:"pergunta"`
}

type fonteDTO struct {
	ChunkID      int64   `json:"chunk_id"`
	Documento    string  `json:"documento"`
	Secao        string  `json:"secao,omitempty"`
	Similaridade float64 `json:"similaridade"`
}

type metricasDTO struct {
	TokensPrompt  int     `json:"tokens_prompt"`
	TokensGerados int     `json:"tokens_gerados"`
	TokensPorSeg  float64 `json:"tokens_por_seg"`
	TempoTotalS   float64 `json:"tempo_total_s"`
}

type askResponse struct {
	Resposta string       `json:"resposta"`
	Fontes   []fonteDTO   `json:"fontes"`
	Metricas *metricasDTO `json:"metricas,omitempty"` // ponteiro: some do JSON se nil
}

// ask trata POST /ask
// Body {"pergunta": "..."}
// query: ?verbosidade=true -> inclui as metricas na resposta
func (h *Handler) ask(w http.ResponseWriter, r *http.Request) {
	// decodificar e valida o corpo
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "corpo inválido", http.StatusBadRequest)
		return
	}
	if req.Pergunta == "" {
		http.Error(w, "campo 'pergunta' é obrigatorio", http.StatusBadRequest)
		return
	}
	// chama o SERVICE
	res, err := h.service.Ask(r.Context(), req.Pergunta)
	if err != nil {
		http.Error(w, "erro ao processar: "+err.Error(), http.StatusInternalServerError)
		return
	}
	//montar a resposta
	out := askResponse{Resposta: res.Texto}
	for _, f := range res.Fontes {
		out.Fontes = append(out.Fontes, fonteDTO{
			ChunkID:      f.ChunkID,
			Documento:    f.Documento,
			Secao:        f.Secao,
			Similaridade: f.Similaridade,
		})
	}
	// métricas só quando ?verbose=true
	if r.URL.Query().Get("verbose") == "true" {
		out.Metricas = &metricasDTO{
			TokensPrompt:  res.Metricas.TokensPrompt,
			TokensGerados: res.Metricas.TokensGerados,
			TokensPorSeg:  res.Metricas.TokensPorSeg,
			TempoTotalS:   res.Metricas.TempoTotal.Seconds(),
		}
	}
	// ?format=text → texto puro (as quebras de linha aparecem de verdade;
	// útil pra ler no Thunder Client; o JSON continua sendo o padrão da API)
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, out.Resposta)
		fmt.Fprintln(w, "\n--- fontes ---")
		for _, f := range out.Fontes {
			secao := ""
			if f.Secao != "" {
				secao = " §" + f.Secao
			}
			fmt.Fprintf(w, "[#%d %s%s] similaridade %.3f\n", f.ChunkID, f.Documento, secao, f.Similaridade)
		}
		if out.Metricas != nil {
			fmt.Fprintf(w, "\n--- métricas ---\ntokens_prompt:  %d\ntokens_gerados: %d\ntok/s:          %.1f\ntempo_total:    %.1fs\n",
				out.Metricas.TokensPrompt, out.Metricas.TokensGerados,
				out.Metricas.TokensPorSeg, out.Metricas.TempoTotalS)
		}
		return
	}

	// responder JSON (padrão)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
