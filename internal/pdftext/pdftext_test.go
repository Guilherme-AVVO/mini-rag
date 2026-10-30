package pdftext

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractArquivoInexistente garante que um caminho inválido falha cedo,
// com erro claro, sem nem tentar rodar o pdftotext.
func TestExtractArquivoInexistente(t *testing.T) {
	_, err := Extract("/caminho/que/nao/existe.pdf")
	if err == nil {
		t.Fatal("esperava erro para arquivo inexistente, veio nil")
	}
	if !strings.Contains(err.Error(), "não encontrado") {
		t.Errorf("erro deveria mencionar 'não encontrado'; veio: %v", err)
	}
}

// TestExtractPDFReal é um teste de integração: só roda se o pdftotext estiver
// instalado E a variável PDF_TEST_DIR apontar para uma pasta com PDFs. Assim o
// `go test` continua passando em máquinas sem o poppler ou sem os PDFs.
func TestExtractPDFReal(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext não está no PATH; pulando teste de integração")
	}

	dir := os.Getenv("PDF_TEST_DIR")
	if dir == "" {
		t.Skip("PDF_TEST_DIR não definido; pulando teste de integração")
	}
	pdfs, err := filepath.Glob(filepath.Join(dir, "*.pdf"))
	if err != nil || len(pdfs) == 0 {
		t.Skipf("nenhum PDF em %s; pulando teste de integração", dir)
	}

	paginas, err := Extract(pdfs[0])
	if err != nil {
		t.Fatalf("Extract(%s) falhou: %v", pdfs[0], err)
	}
	if len(paginas) < 1 {
		t.Fatalf("esperava ≥1 página em %s, veio %d", pdfs[0], len(paginas))
	}
	// Contrato: o \f é o separador e NUNCA deve sobrar dentro de uma página.
	for i, p := range paginas {
		if strings.Contains(p, "\f") {
			t.Errorf("página %d ainda contém \\f", i+1)
		}
	}
}
