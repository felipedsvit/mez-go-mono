//go:build integration
// +build integration

// Package chaos — harness.go: utilidades para testes de chaos (Fase 8 #106).
//
// Os testes sob este package sobem o binário mez-go-mono real, enviam
// sinais, matam processos e validam que a próxima instância recupera o
// estado. Requer docker (testcontainers) e Go toolchain.
package chaos

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Harness encapsula um processo mez-go-mono em execução.
type Harness struct {
	BinPath string
	Env     []string
	Cmd     *exec.Cmd
	Cancel  context.CancelFunc
	WorkDir string
}

// Build compila o binário em /tmp/chaos-mez-<pid>/server.
func Build(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "mez-go-mono")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/server")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	return binPath
}

// Start compila (se necessário) e inicia o binário com env vars.
// Captura stdout/stderr em arquivos em tmpDir.
func Start(t *testing.T, env ...string) *Harness {
	t.Helper()
	if testing.Short() {
		t.Skip("chaos")
	}
	binPath := Build(t)
	tmpDir := t.TempDir()
	stdout, err := os.Create(filepath.Join(tmpDir, "stdout.log"))
	if err != nil {
		t.Fatalf("create stdout: %v", err)
	}
	stderr, err := os.Create(filepath.Join(tmpDir, "stderr.log"))
	if err != nil {
		t.Fatalf("create stderr: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath, "serve")
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}

	h := &Harness{
		BinPath: binPath,
		Env:     env,
		Cmd:     cmd,
		Cancel:  cancel,
		WorkDir: tmpDir,
	}
	t.Cleanup(func() { h.Stop(true) })
	return h
}

// Kill9 envia SIGKILL ao processo. Não espera.
func (h *Harness) Kill9() {
	if h.Cmd == nil || h.Cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-h.Cmd.Process.Pid, syscall.SIGKILL)
}

// Stop termina o processo. Se graceful=true, envia SIGTERM e espera
// exit. Se false, envia SIGKILL.
func (h *Harness) Stop(graceful bool) {
	if h.Cmd == nil || h.Cmd.Process == nil {
		return
	}
	if graceful {
		_ = syscall.Kill(-h.Cmd.Process.Pid, syscall.SIGTERM)
		_, _ = h.Cmd.Process.Wait()
	} else {
		_ = syscall.Kill(-h.Cmd.Process.Pid, syscall.SIGKILL)
		_, _ = h.Cmd.Process.Wait()
	}
	if h.Cancel != nil {
		h.Cancel()
	}
}

// WaitReady polling em /readyz até 200 ou timeout. Retorna erro se timeout.
func (h *Harness) WaitReady(timeout time.Duration) error {
	addr := os.Getenv("MEZ_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + strings.Split(addr, ":")[1]
	}
	url := "http://localhost" + addr + "/readyz"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("readyz timeout after %v", timeout)
}

// ReadStdout lê o conteúdo do stdout.log.
func (h *Harness) ReadStdout() string {
	data, _ := os.ReadFile(filepath.Join(h.WorkDir, "stdout.log"))
	return string(data)
}

// FreePort retorna uma porta TCP livre.
func FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// WaitForReconcile polling no banco até que `expected` mensagens estejam
// processadas (status != 'received') ou timeout.
func WaitForReconcile(t *testing.T, dbURL string, expected int, timeout time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count, err := countMessages(t, dbURL, "status='received'")
		if err == nil && count <= int64(expected) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("reconcile timeout: expected ≤ %d received, took %v", expected, timeout)
}

// assertZeroPending: helper para contar mensagens em dado status.
func countMessages(t *testing.T, dbURL, where string) (int64, error) {
	t.Helper()
	cmd := exec.Command("psql", dbURL, "-tAc", "SELECT COUNT(*) FROM messages WHERE "+where)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var n int64
	_, _ = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n, nil
}

// ScanLog lê um arquivo de log linha a linha até encontrar match ou EOF.
// Útil para validar que mensagens esperadas foram logadas.
func ScanLog(path, match string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if strings.Contains(line, match) {
			return line, nil
		}
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", err
		}
	}
}
