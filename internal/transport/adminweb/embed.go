package adminweb

import "embed"

//go:embed templates/*.html
var TemplatesFS embed.FS

func Templates() embed.FS {
	return TemplatesFS
}
