package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"

	"github.com/Guilherme-AVVO/mini-rag/internal/config"
)

// New cria e devolve um pool de conexões com o banco de dados PostgreSQL(pgvector).
// recebe o ctx (para controle de tempo e cancelamento) e a configuração do banco de dados
// recebe a config ja carregada.
func New(ctx context.Context, Cfg *config.Config) (*pgxpool.Pool, error) {
	// Constrói a string de conexão usando as variáveis de ambiente
	DSN := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		Cfg.PG_USER, Cfg.PG_PASSWORD, Cfg.PG_HOST, Cfg.PG_PORT, Cfg.PG_DB)

	//ParseConfig em vez de pgxpool.New direto:
	//edição de config para add o tipo de vetor do pgvector
	pgxCfg, err := pgxpool.ParseConfig(DSN)
	if err != nil {
		return nil, fmt.Errorf("error parsing database config: %w", err)
	}

	//registrar o tipo vector a cada conexão que o pool abrir
	// necessario para o pgvector funcionar corretamente
	pgxCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	//configs de pool de conexões
	pgxCfg.MaxConns = 10
	pgxCfg.MaxConnIdleTime = 5 * time.Minute
	// gerando pool de conexões com a configuração editada
	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating database connection pool: %w", err)
	}

	// Testa a conexão com o banco de dados(com timeout para não trava no boot)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("error pinging database: %w", err)
	}

	return pool, nil
}
