// Package pdftext extrai o texto de PDFs usando o utilitário pdftotext
// (poppler-utils), preservando a divisão por páginas.
//
// Por que delegar ao pdftotext em vez de parsear PDF em Go: extrair texto de
// PDF "na unha" é notoriamente difícil (fontes embutidas, encodings, layout em
// colunas). O poppler resolve isso há décadas, e a flag -layout preserva o
// alinhamento visual das tabelas — essencial para fichas técnicas, onde o
// pareamento coluna↔valor carrega o significado.
//
// O contrato com o resto do pipeline é o marcador de página: o pdftotext emite
// um form-feed (\f) ao FIM de cada página, e este pacote devolve as páginas já
// separadas (sem o \f dentro delas), prontas para chunking com numeração
// estável de página.
package pdftext

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Extract extrai o texto de um PDF com pdftotext -layout (poppler-utils),
// devolvendo uma fatia com o texto de cada página.
func Extract(pdfPath string) ([]string, error) {
	// Valida o arquivo ANTES de chamar o pdftotext: o erro do poppler para
	// arquivo inexistente é genérico ("I/O Error"); aqui damos um erro claro
	// e em português, com o caminho exato que falhou.
	if _, err := os.Stat(pdfPath); err != nil {
		return nil, fmt.Errorf("pdftext: arquivo não encontrado: %s: %w", pdfPath, err)
	}

	// Verifica a presença do binário separadamente para poder orientar a
	// instalação — "executable file not found" sozinho não diz ao usuário
	// QUAL pacote do sistema instalar.
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return nil, fmt.Errorf("pdftext: pdftotext não encontrado no PATH; instale o poppler-utils (ex.: sudo apt install poppler-utils): %w", err)
	}

	// -layout  → preserva o layout visual (tabelas das fichas técnicas)
	// -enc UTF-8 → saída em UTF-8, independente do locale da máquina
	// "-"      → escreve no stdout em vez de criar um .txt no disco
	// NÃO usamos -nopgbrk: o \f ao fim de cada página é o nosso marcador.
	cmd := exec.Command("pdftotext", "-layout", "-enc", "UTF-8", pdfPath, "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// O stderr do poppler costuma dizer o motivo real (PDF corrompido,
		// senha, etc.) — incluímos na mensagem para o diagnóstico ser direto.
		return nil, fmt.Errorf("pdftext: pdftotext falhou para %s: %w: %s",
			pdfPath, err, strings.TrimSpace(stderr.String()))
	}

	// O pdftotext emite \f ao FINAL de cada página, então o Split deixa um
	// elemento vazio sobrando no fim ("p1\fp2\f" → ["p1","p2",""]). Removemos
	// apenas os vazios FINAIS: páginas vazias no MEIO do documento são
	// preservadas para não desalinhar a numeração de páginas.
	paginas := strings.Split(stdout.String(), "\f")
	for len(paginas) > 0 && paginas[len(paginas)-1] == "" {
		paginas = paginas[:len(paginas)-1]
	}
	return paginas, nil
}
