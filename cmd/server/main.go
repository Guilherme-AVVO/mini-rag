package main

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Guilherme-AVVO/mini-rag/internal/config"
	"github.com/Guilherme-AVVO/mini-rag/internal/database"
	"github.com/Guilherme-AVVO/mini-rag/internal/embed"
	"github.com/Guilherme-AVVO/mini-rag/internal/handler"
	"github.com/Guilherme-AVVO/mini-rag/internal/llm"
	"github.com/Guilherme-AVVO/mini-rag/internal/rag"
	"github.com/Guilherme-AVVO/mini-rag/internal/repository"
)

func main() {
	ctx := context.Background()
	cfg := config.LoadConfig()

	// 1) banco (pool de conexões)
	pool, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatal("banco: ", err)
	}
	defer pool.Close()

	// 2) dependências (wiring) — o único lugar que monta tudo
	repo := repository.New(pool)

	emb, err := embed.New(cfg.LLM_URL, cfg.EMBED_MODEL)
	if err != nil {
		log.Fatal("embed: ", err)
	}
	gen, err := llm.New(cfg.LLM_URL, cfg.LLM_MODEL)
	if err != nil {
		log.Fatal("llm: ", err)
	}
	topK, err := strconv.Atoi(cfg.TOP_K)
	if err != nil {
		log.Fatal("TOP_K inválido: ", err)
	}

	// 3) service + handler
	service := rag.New(emb, repo, gen, topK, cfg.RETRIEVAL_MODE)
	log.Printf("retrieval mode: %s", cfg.RETRIEVAL_MODE)
	// modo 2-agentes (Pesquisador -> multi-query -> Redator), atrás de flag:
	// AGENT_MODE=off mantém o pipeline 1-passo exatamente como era.
	if cfg.AGENT_MODE == "on" {
		mq, err := strconv.Atoi(cfg.MULTI_QUERY_N)
		if err != nil {
			log.Fatal("MULTI_QUERY_N inválido: ", err)
		}
		atk, err := strconv.Atoi(cfg.AGENT_TOPK)
		if err != nil {
			log.Fatal("AGENT_TOPK inválido: ", err)
		}
		service.EnableAgents(mq, atk)
		log.Printf("agent mode: on (multi_query=%d, agent_topk=%d)", mq, atk)
	} else {
		log.Printf("agent mode: off")
	}
	h := handler.New(service)

	// 4) router chi
	r := chi.NewRouter()
	r.Use(middleware.Logger) // loga cada requisição no terminal
	r.Mount("/", h.Router())

	// 5) sobe o servidor HTTP
	addr := ":" + cfg.HTTP_PORT
	log.Println("servidor ouvindo em", addr)
	log.Fatal(http.ListenAndServe(addr, r))

}
