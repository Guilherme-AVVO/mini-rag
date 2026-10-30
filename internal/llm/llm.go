// Package llm é o gateway de geração: fala com o ollama (qwen3 / gemma3)
// e devolve a resposta de texto E as métricas de desempenho da geração.
package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
)

// Metricas resume os números de desempenho que o ollama devolve (os mesmos
// do "--verbose" do CLI, mas vindos nativamente pela API).
type Metricas struct {
	TokensPrompt  int           // tokens de entrada (prompt)
	TokensGerados int           // tokens de saída
	TempoTotal    time.Duration // tempo total da requisição
	TempoGeracao  time.Duration // tempo de geração efetivo dos tokens de saída
	TokensPorSeg  float64       // EvalCount / EvalDuration -> o "tok/s"
}

// Resposta é o retorno da geração: texto e métricas.
type Resposta struct {
	Texto    string
	Metricas Metricas
}

// Gerador é o gateway de geração.
type Gerador struct {
	Client *api.Client
	Model  string
}

// New cria o Gerador. baseURL = LLM_URL; model = nome do modelo (qwen3/gemma3).
func New(baseURL, model string) (*Gerador, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse baseURL: %w", err)
	}
	return &Gerador{Client: api.NewClient(u, http.DefaultClient), Model: model}, nil
}

// Generate envia system + prompt ao modelo e devolve a resposta completa + métricas.
// A API do ollama é por callback; com Stream=false ela chama o callback UMA vez,
// já com a resposta completa e Done=true (onde vêm as métricas).
func (g *Gerador) Generate(ctx context.Context, system, prompt string) (Resposta, error) {
	naoStream := false // resposta inteira de uma vez (não streaming)

	// O modelo custom qwen3-14b-teste tem TEMPLATE sobrescrito sem suporte ao
	// parâmetro "think" da API; o interruptor que funciona é o soft switch
	// /no_think no system prompt (é também o que os critérios v2 prescrevem
	// para o Qwen3). Para os demais modelos, não muda nada.
	if strings.Contains(strings.ToLower(g.Model), "qwen") {
		system = "/no_think\n" + system
	}

	var out Resposta
	var sb strings.Builder

	err := g.Client.Generate(ctx, &api.GenerateRequest{
		Model:  g.Model,
		System: system,
		Prompt: prompt,
		Stream: &naoStream,
		Think:  &api.ThinkValue{Value: false}, // desliga o "think" do qwen3 (gemma3 ignora)
	}, func(r api.GenerateResponse) error {
		sb.WriteString(r.Response)
		if r.Done {
			m := Metricas{
				TokensPrompt:  r.PromptEvalCount,
				TokensGerados: r.EvalCount,
				TempoTotal:    r.TotalDuration,
				TempoGeracao:  r.EvalDuration,
			}
			if r.EvalDuration > 0 {
				m.TokensPorSeg = float64(r.EvalCount) / r.EvalDuration.Seconds()
			}
			out.Metricas = m
		}
		return nil
	})
	if err != nil {
		return Resposta{}, fmt.Errorf("generate no ollama: %w", err)
	}

	texto := sb.String()
	// Templates sem suporte a think deixam as tags <think>...</think> cruas no
	// texto (mesmo vazias com /no_think). Remove o bloco antes de devolver.
	if i := strings.Index(texto, "</think>"); i != -1 {
		texto = texto[i+len("</think>"):]
	}
	out.Texto = strings.TrimSpace(texto)
	return out, nil
}
