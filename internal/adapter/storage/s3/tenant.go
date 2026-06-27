// Package s3 — tenant.go: helpers para S3 com prefixo de tenant.
//
// Issue #138 (Sprint 0A C10 audit, DREAD 8.0): Put/Get/Delete aceitam
// `key` cru. Handler com tenantID=A pode passar key="tenants/B/media/x.png"
// e gravar no prefixo do tenant B (path confusion cross-tenant).
//
// Fix: helper WithTenantPrefix(tenantID, key) constrói o path
// "tenants/<tenantID>/<key>" e valida que o resultado não tem
// traversal (..) ou escape. Rejeita com ErrTenantMismatch se o key
// tentar escapar do prefixo.
package s3

import (
	"errors"
	"path"
	"strings"
)

// ErrTenantMismatch é retornado quando o key tenta acessar path de
// outro tenant (path traversal ou prefixo absoluto).
var ErrTenantMismatch = errors.New("s3: key escapes tenant prefix")

// tenantPrefix é o prefixo canônico de todos os objetos multi-tenant.
// Single-tenant (ex: system_settings backup) usa prefixos diferentes
// que não passam por WithTenantPrefix.
const tenantPrefix = "tenants/"

// WithTenantPrefix monta o full key para um objeto de tenant, garantindo
// que key não escapa do prefixo tenants/<tenantID>/.
//
// Uso:
//
//	fullKey, err := s3.WithTenantPrefix(tenantID, "media/x.png")
//	if err != nil { return err }
//	store.Put(ctx, fullKey, data, mime)
//
// Rejeita:
//   - tenantID vazio
//   - key que começa com "/" (absoluto)
//   - key com ".." (path traversal)
//   - key que após join tem prefixo diferente de "tenants/<tenantID>/"
//
// Defesa em profundidade: depois de path.Join, validamos que o resultado
// ainda começa com "tenants/<tenantID>/". Se o join normalizou o path de
// forma a escapar (ex: "../../tenants/B"), o prefixo não vai bater.
func WithTenantPrefix(tenantID, key string) (string, error) {
	if tenantID == "" {
		return "", ErrTenantMismatch
	}
	if key == "" {
		return "", ErrTenantMismatch
	}
	if strings.HasPrefix(key, "/") {
		return "", ErrTenantMismatch
	}
	if strings.Contains(key, "..") {
		return "", ErrTenantMismatch
	}

	full := path.Join(tenantPrefix+tenantID, key)
	expected := tenantPrefix + tenantID + "/"
	if !strings.HasPrefix(full, expected) {
		return "", ErrTenantMismatch
	}
	return full, nil
}

// MustWithTenantPrefix é a versão que panica. Usar apenas em paths de
// código com tenantID garantido (ex: início de request após auth).
func MustWithTenantPrefix(tenantID, key string) string {
	full, err := WithTenantPrefix(tenantID, key)
	if err != nil {
		panic("s3.MustWithTenantPrefix: " + err.Error())
	}
	return full
}
