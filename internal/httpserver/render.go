package httpserver

import (
	"bytes"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"errgotrack/web"
)

// renderer holds one *template.Template clone per top-level page, each
// seeded with the shared base layout and partials plus that page's own
// content block - see the package doc comment below for why the clone is
// necessary.
type renderer struct {
	mu     sync.RWMutex
	pages  map[string]*template.Template
	logger *slog.Logger
}

// Go's html/template shares every {{define "name"}} across one parsed set,
// so if every page defined a block named "content" they'd overwrite each
// other. Instead we parse the shared base+partials once, then Clone() that
// root for each page and parse only that page's file into the clone - each
// page's "content"/"pagetitle" block lives in its own isolated template set.
func newRenderer(logger *slog.Logger) (*renderer, error) {
	root := template.New("root").Funcs(funcMap())
	root, err := root.ParseFS(web.Templates, "templates/base.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(web.Templates, "templates")
	if err != nil {
		return nil, err
	}

	r := &renderer{pages: map[string]*template.Template{}, logger: logger}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "base.html" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		clone, err := root.Clone()
		if err != nil {
			return nil, err
		}
		clone, err = clone.ParseFS(web.Templates, "templates/"+e.Name())
		if err != nil {
			return nil, err
		}
		r.pages[name] = clone
	}
	return r, nil
}

// Page renders a full HTML page (page name without extension, e.g.
// "dashboard") via the shared "base" layout.
func (r *renderer) Page(w http.ResponseWriter, name string, data any) {
	r.exec(w, name, "base", data)
}

// Fragment renders a single named block from a page's template set, used
// for HTMX partial swaps (e.g. "issues_table").
func (r *renderer) Fragment(w http.ResponseWriter, page, block string, data any) {
	r.exec(w, page, block, data)
}

func (r *renderer) exec(w http.ResponseWriter, page, block string, data any) {
	r.mu.RLock()
	tmpl, ok := r.pages[page]
	r.mu.RUnlock()
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, block, data); err != nil {
		r.logger.Error("template render failed", "page", page, "block", block, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

var iconCache sync.Map // name -> template.HTML

func funcMap() template.FuncMap {
	return template.FuncMap{
		"icon": iconFunc,
	}
}

// iconFunc inlines a vendored Lucide SVG by name, applying the given CSS
// class, so icons render without a separate font/JS icon library.
func iconFunc(name, class string) template.HTML {
	if cached, ok := iconCache.Load(name); ok {
		return applyClass(cached.(string), class)
	}
	data, err := fs.ReadFile(web.Static, "static/icons/"+name+".svg")
	if err != nil {
		return template.HTML("")
	}
	svg := string(data)
	if idx := strings.Index(svg, "-->"); strings.HasPrefix(strings.TrimSpace(svg), "<!--") && idx >= 0 {
		svg = svg[idx+3:]
	}
	iconCache.Store(name, svg)
	return applyClass(svg, class)
}

// applyClass adds a CSS class to the root <svg> element. Every vendored
// Lucide icon already ships its own class="lucide lucide-x" attribute, so
// blindly inserting a second class="..." produced a duplicate attribute on
// every single icon in the app - invalid HTML, and since a parser keeps
// only the first occurrence of a duplicate attribute, Lucide's own class
// was silently dropped on every render. This merges into the existing
// attribute when there is one, and only falls back to inserting a fresh
// one for an SVG that doesn't already have a class.
func applyClass(svg, class string) template.HTML {
	const attr = `class="`
	if start := strings.Index(svg, attr); start != -1 {
		valueStart := start + len(attr)
		if end := strings.Index(svg[valueStart:], `"`); end != -1 {
			existing := svg[valueStart : valueStart+end]
			return template.HTML(svg[:valueStart] + class + " " + existing + svg[valueStart+end:])
		}
	}
	return template.HTML(strings.Replace(svg, "<svg", `<svg class="`+class+`"`, 1))
}
