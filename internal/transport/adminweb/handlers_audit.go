// Package adminweb — handlers_audit.go: handler /admin/audit.
package adminweb

import (
	"net/http"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Audit Log"
	// Issue #154 (M8 audit, Sprint 0C): crossTenant false por default
	// (handler de UI não passa pela authz gate de platform — caller
	// é tipicamente tenant owner). Requer tenantID do query param.
	tenantID := r.URL.Query().Get("tenant_id")
	entries, err := s.audit.List(r.Context(), cdomain.AuditFilter{TenantID: tenantID, Limit: 100}, false)
	if err != nil {
		p.Error = "Error loading audit log"
		s.renderTempl(w, templates.Audit(templates.AuditData{Page: p, Entries: nil}))
		return
	}
	s.renderTempl(w, templates.Audit(templates.AuditData{Page: p, Entries: entries}))
}
