package adminweb

import (
	"net/http"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	entries, err := s.audit.List(r.Context(), cdomain.AuditFilter{Limit: 100})
	if err != nil {
		s.renderPage(w, "audit.html", PageData{Title: "Audit Log", Error: "Error loading audit log", Now: time.Now(), Principal: principal})
		return
	}

	data := PageData{
		Title:     "Audit Log",
		Principal: principal,
		Data:      entries,
		Now:       time.Now(),
	}
	s.renderPage(w, "audit.html", data)
}
