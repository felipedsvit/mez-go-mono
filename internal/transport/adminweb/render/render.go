package render

import (
	"html/template"
	"io"
	"io/fs"
	"sync"
)

type Renderer struct {
	mu    sync.RWMutex
	cache map[string]*template.Template
	base  string
	funcs template.FuncMap
}

func New(base string, funcs template.FuncMap) *Renderer {
	if funcs == nil {
		funcs = template.FuncMap{}
	}
	return &Renderer{
		cache: make(map[string]*template.Template),
		base:  base,
		funcs: funcs,
	}
}

func (r *Renderer) Render(w io.Writer, fsys fs.FS, page string, data any) error {
	tpl, err := r.load(fsys, page)
	if err != nil {
		return err
	}
	return tpl.ExecuteTemplate(w, r.base, data)
}

func (r *Renderer) RenderFragment(w io.Writer, fsys fs.FS, page, fragment string, data any) error {
	tpl, err := r.load(fsys, page)
	if err != nil {
		return err
	}
	return tpl.ExecuteTemplate(w, fragment, data)
}

func (r *Renderer) load(fsys fs.FS, page string) (*template.Template, error) {
	r.mu.RLock()
	tpl, ok := r.cache[page]
	r.mu.RUnlock()
	if ok {
		return tpl, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if tpl, ok = r.cache[page]; ok {
		return tpl, nil
	}

	tpl, err := template.New(r.base).Funcs(r.funcs).ParseFS(fsys, "templates/base.html", "templates/"+page)
	if err != nil {
		return nil, err
	}

	r.cache[page] = tpl
	return tpl, nil
}
