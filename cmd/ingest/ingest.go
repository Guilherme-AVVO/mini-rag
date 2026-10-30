package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Guilherme-AVVO/mini-rag/internal/chunk"
	"github.com/Guilherme-AVVO/mini-rag/internal/config"
	"github.com/Guilherme-AVVO/mini-rag/internal/database"
	"github.com/Guilherme-AVVO/mini-rag/internal/embed"
	"github.com/Guilherme-AVVO/mini-rag/internal/repository"
)

func main() {
	// -doc: quando informado, processa só kb/clean/<doc>.txt (atualização pontual)
	doc := flag.String("doc", "", "nome do documento (sem .txt) para ingerir só kb/clean/<doc>.txt")
	flag.Parse()

	// config .env
	ctx := context.Background()

	// Carrega configuração
	cfg := config.LoadConfig()

	chunkSize, err := strconv.Atoi(cfg.CHUNK_SIZE) // CHUNK_SIZE é string, converte para int
	if err != nil {
		log.Fatalf("Erro ao converter CHUNK_SIZE: %v", err)
	}

	// Dependências: banco(pool) + gateways
	pool, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco: %v", err)
	}
	defer pool.Close()

	repo := repository.New(pool)

	emb, err := embed.New(cfg.LLM_URL, cfg.EMBED_MODEL)
	if err != nil {
		log.Fatalf("Erro ao criar embedder: %v", err)
	}

	// Lista os textos já limpos da base de conheimento
	// (com -doc, processa só aquele arquivo; sem -doc, o glob inteiro)
	var arquivos []string
	if *doc != "" {
		caminho := filepath.Join("kb", "clean", *doc+".txt")
		if _, err := os.Stat(caminho); err != nil {
			log.Fatalf("Documento %q não encontrado (%s): %v", *doc, caminho, err)
		}
		arquivos = []string{caminho}
	} else {
		arquivos, err = filepath.Glob("kb/clean/*.txt")
		if err != nil {
			log.Fatalf("Erro ao listar arquivos: %v", err)
		}
		if len(arquivos) == 0 {
			log.Fatalf("Nenhum arquivo encontrado em kb/clean")
		}
	}

	inicio := time.Now()
	total := 0

	// processar arquivos por arquivos
	for _, caminho := range arquivos {
		conteudo, err := os.ReadFile(caminho)
		if err != nil {
			log.Printf("Erro ao ler arquivo %s: %v", caminho, err)
			continue
		}

		documento := strings.TrimSuffix(filepath.Base(caminho), ".txt") // nome do doc sem extensão

		// re-ingestão idempotente: remove os chunks antigos do documento antes de inserir
		removidos, err := repo.DeleteByDocumento(ctx, documento)
		if err != nil {
			log.Fatalf("Erro ao remover chunks antigos de %s: %v", documento, err)
		}
		if removidos > 0 {
			log.Printf("  %s: %d chunks antigos removidos (re-ingestão)", documento, removidos)
		}

		pedacos := chunk.Split(string(conteudo), chunkSize)
		log.Printf(" %s -> %d", documento, len(pedacos))

		// para cada chunk; gera o embedding e grava
		for i, ch := range pedacos {
			Vetor, err := emb.Embed(ctx, ch.Conteudo)
			if err != nil {
				log.Fatalf("embedding (%s #%d): %v", documento, i, err)
			}
			err = repo.Insert(ctx, repository.Chunk{
				Documento: documento,
				Secao:     ch.Secao,                         // seção da norma detectada no chunking
				Pagina:    ch.Pagina,                        // página (1-based) onde o chunk começa
				Ordem:     i,                                //posição do chunk no documento
				Conteudo:  ch.Conteudo,                      // o texto que volta pro LLM
				Tokens:    len(strings.Fields(ch.Conteudo)), // contagem simples de palavras
				Embedding: Vetor,                            // []float32 de 1024
			})
			if err != nil {
				log.Fatalf("Erro ao inserir (%s #%d): %v", documento, i, err)
			}
			total++
		}
	}
	escopo := fmt.Sprintf("%d arquivo(s)", len(arquivos))
	if *doc != "" {
		escopo = fmt.Sprintf("documento %q", *doc)
	}
	log.Printf("Ingestão concluída: %d chunks de %s em %s", total, escopo, time.Since(inicio).Round(time.Millisecond))

}
