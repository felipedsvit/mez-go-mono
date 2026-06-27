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
	entries, err := s.audit.List(r.Context(), cdomain.AuditFilter{Limit: 100})
	if err != nil {
		p.Error = "Error loading audit log"
		s.renderTempl(w, templates.Audit(templates.AuditData{Page: p, Entries: nil}))
		return
	}
	s.renderTempl(w, templates.Audit(templates.AuditData{Page: p, Entries: entries}))
}
