package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	PG_USER     string
	PG_PASSWORD string
	PG_DB       string
	PG_PORT     string
	PG_HOST     string

	LLM_URL     string
	LLM_MODEL   string
	EMBED_MODEL string

	CHUNK_SIZE string
	TOP_K      string

	HTTP_PORT string

	RETRIEVAL_MODE string // "semantic" (baseline) | "contextual" (embedding_ctx)

	AGENT_MODE    string // "off" (pipeline 1-passo atual) | "on" (Pesquisador -> multi-query -> Redator)
	MULTI_QUERY_N string // máx. de sub-buscas geradas pelo Pesquisador (default 3)
	AGENT_TOPK    string // chunks recuperados POR sub-busca antes do merge (default TOP_K)
}

func LoadConfig() *Config {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	Cfg := &Config{
		PG_USER:     os.Getenv("POSTGRES_USER"),
		PG_PASSWORD: os.Getenv("POSTGRES_PASSWORD"),
		PG_DB:       os.Getenv("POSTGRES_DB"),
		PG_PORT:     os.Getenv("POSTGRES_PORT"),
		PG_HOST:     os.Getenv("POSTGRES_HOST"),

		LLM_URL:     os.Getenv("LLM_URL"),
		LLM_MODEL:   os.Getenv("LLM_MODEL"),
		EMBED_MODEL: os.Getenv("EMBED_MODEL"),

		CHUNK_SIZE: (os.Getenv("CHUNK_SIZE")),
		TOP_K:      (os.Getenv("TOP_K")),

		HTTP_PORT: os.Getenv("HTTP_PORT"),

		RETRIEVAL_MODE: os.Getenv("RETRIEVAL_MODE"),

		AGENT_MODE:    os.Getenv("AGENT_MODE"),
		MULTI_QUERY_N: os.Getenv("MULTI_QUERY_N"),
		AGENT_TOPK:    os.Getenv("AGENT_TOPK"),
	}
	if Cfg.RETRIEVAL_MODE == "" {
		Cfg.RETRIEVAL_MODE = "semantic" // default = baseline
	}
	if Cfg.AGENT_MODE == "" {
		Cfg.AGENT_MODE = "off" // default = pipeline atual (1 passo), nada muda
	}
	if Cfg.MULTI_QUERY_N == "" {
		Cfg.MULTI_QUERY_N = "3"
	}
	if Cfg.AGENT_TOPK == "" {
		Cfg.AGENT_TOPK = Cfg.TOP_K // por sub-busca, igual ao TOP_K se não configurado
	}
	return Cfg
}
