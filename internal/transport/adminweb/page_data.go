package adminweb

import (
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type PageData struct {
	Title      string
	Principal  admin.Principal
	Error      string
	Success    string
	Now        time.Time
	StaticBase string
	CSRFToken  string
	Data       any
}
