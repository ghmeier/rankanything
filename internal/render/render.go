// Package render owns the template cache and the page-vs-partial dispatch
// that makes htmx swaps cheap.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

// Renderer holds every parsed template.
type Renderer struct {
	tmpl *template.Template
}

// New parses every template in fsys (expects pages/ and partials/ plus
// layout.html at the root).
func New(fsys fs.FS) (*Renderer, error) {
	r := &Renderer{}
	t := template.New("main").Funcs(funcs(r))
	t, err := t.ParseFS(fsys, "layout.html", "pages/*.html", "partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("render: parse templates: %w", err)
	}
	r.tmpl = t
	return r, nil
}

func funcs(r *Renderer) template.FuncMap {
	return template.FuncMap{
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, fmt.Errorf("dict: odd argument count")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
		"formatTime": func(t any) string {
			switch v := t.(type) {
			case interface {
				Time() (interface{ Format(string) string }, bool)
			}:
				if tm, ok := v.Time(); ok && tm != nil {
					return tm.Format("Jan 2, 2006")
				}
				return ""
			default:
				return fmt.Sprintf("%v", t)
			}
		},
		"renderDynamic": func(name string, data any) (template.HTML, error) {
			var buf bytes.Buffer
			err := r.tmpl.ExecuteTemplate(&buf, name, data)
			t := template.HTML(buf.String())
			return t, err
		},
	}
}

// Page renders a full document: the named page template wrapped in layout.html.
func (r *Renderer) Page(w http.ResponseWriter, status int, page string, data any) error {
	return r.exec(w, status, "layout.html", pageData{Page: page, Data: data})
}

// Partial renders a single named template with no layout — what htmx swaps in.
func (r *Renderer) Partial(w http.ResponseWriter, status int, name string, data any) error {
	return r.exec(w, status, name, data)
}

func (r *Renderer) Empty(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

type pageData struct {
	Page string
	Data any
}

func (r *Renderer) exec(w http.ResponseWriter, status int, name string, data any) error {
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("render %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// IsHTMX reports whether the request wants a fragment rather than a page.
func IsHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
