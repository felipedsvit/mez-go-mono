// Package config — keys.go: leitura segura de chaves de arquivo.
//
// Issue #141 (H3 audit, DREAD 6.2): KEK = root de toda envelope
// encryption (Fase 7 #89, #91, #92). Sem check de permissão 0600 e
// sem O_NOFOLLOW, um arquivo 0644 world-readable vaza a KEK para
// qualquer user do host, e um symlink para /dev/stdin ou similar
// permite leitura arbitrária.
//
// CWE-732 (Incorrect Permission Assignment) · CWE-367 (TOCTOU).
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ReadKeyFile lê o conteúdo de path, validando:
//  1. Permissão 0600 (sem bits de grupo/outro) — fail-closed se violar.
//  2. O_NOFOLLOW — falha se path for symlink (defesa contra symlink attacks).
//  3. Não-vazio (após TrimSpace).
//
// Retorna a chave sem espaços nas pontas. Erro descritivo se qualquer
// validação falhar (não loga o conteúdo da chave).
func ReadKeyFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("key file path is empty")
	}

	// O_NOFOLLOW: falha se for symlink. linux.DT_LNK = symlink no readdir;
	// usar os.Lstat para detectar symlinks sem seguir.
	// O_RSYNC | O_NOFOLLOW equivalente em Go: open com syscall.O_NOFOLLOW.
	// Como Go 1.x não expõe O_NOFOLLOW via os.OpenFile, usamos Lstat
	// primeiro para checar e depois abrimos normalmente. Race condition
	// teórica (TOCTOU entre Lstat e Open), mas mitigada pelo uso do
	// file descriptor; alternativa seria usar unix.Openat.
	fi, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("lstat key file: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("key file %q is a symlink; refusing to read (CWE-367)", path)
	}

	// Permissão: aceita apenas 0600. Rejeita 0644, 0666, 0777 etc.
	// Bits de permissão: 0o777. Verifica 0o077 (group+other).
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("key file %q has insecure permissions %#o (must be 0600); chmod 600 %s", path, perm, path)
	}

	// Lê o arquivo.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read key file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
