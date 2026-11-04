package report

import (
	"html/template"
	"io"
	"slices"
	"strings"
)

// page is a doc as the HTML template shows it, each cell with its classes.
type page struct {
	Title  string
	Notes  []string
	Groups []pageGroup
}

// pageGroup is a group; Needs marks the one that needs the user.
type pageGroup struct {
	Title    string
	Needs    bool
	Sections []pageSection
}

// pageSection is a section; Box tints one that needs the user.
type pageSection struct {
	Title string
	Box   bool
	Parts []pagePart
}

// pagePart is a part: a line, code to paste or a table.
type pagePart struct {
	Line  string
	Code  string
	Table *pageTable
}

// pageTable is a table; a muted one is shown low-key.
type pageTable struct {
	Muted bool
	Head  []cell
	Rows  [][]cell
}

// cell is a table cell's text and its classes.
type cell struct {
	Text  string
	Class string
}

// details are the columns naming what decided or identifying a receipt, shown muted.
var details = map[string]bool{"Receipt": true, "Rule": true, "By": true, "Of": true, "Trip": true, "Booking": true, "Source": true, "Why": true}

// writeHTML prints d as an HTML page.
func (d doc) writeHTML(w io.Writer) error { return pageTemplate.Execute(w, d.page()) }

// page is d with each cell's classes: amounts right-aligned, credits green, the last column
// wide, details muted.
func (d doc) page() page {
	p := page{Title: d.header, Notes: d.notes}
	for _, g := range d.groups {
		pg := pageGroup{Title: g.title, Needs: g.needs}
		for _, s := range g.sections {
			ps := pageSection{Title: s.title, Box: g.needs && !s.quiet}
			for _, pt := range s.parts {
				switch {
				case pt.table != nil:
					ps.Parts = append(ps.Parts, pagePart{Table: pt.table.page(s.quiet)})
				case pt.code != nil:
					ps.Parts = append(ps.Parts, pagePart{Code: strings.Join(pt.code, "\n")})
				default:
					ps.Parts = append(ps.Parts, pagePart{Line: pt.line})
				}
			}
			pg.Sections = append(pg.Sections, ps)
		}
		p.Groups = append(p.Groups, pg)
	}
	return p
}

// page is t with each cell's classes; muted shows it low-key.
func (t table) page(muted bool) *pageTable {
	pt := &pageTable{Muted: muted}
	class := func(i int, text string, head bool) string {
		var cs []string
		c := t.cols[i]
		if c.right {
			cs = append(cs, "amt")
			if !head && strings.HasPrefix(text, "+") {
				cs = append(cs, "credit")
			}
		}
		if i == len(t.cols)-1 && !head {
			cs = append(cs, "wide")
			if details[c.name] {
				cs = append(cs, "muted")
			}
		}
		return strings.Join(cs, " ")
	}
	if slices.ContainsFunc(t.cols, func(c column) bool { return c.name != "" }) {
		for i, c := range t.cols {
			pt.Head = append(pt.Head, cell{Text: c.name, Class: class(i, c.name, true)})
		}
	}
	for _, r := range t.rows {
		row := make([]cell, len(r))
		for i, text := range r {
			row[i] = cell{Text: text, Class: class(i, text, false)}
		}
		pt.Rows = append(pt.Rows, row)
	}
	return pt
}

// arrows is text escaped, its arrows dimmed.
func arrows(text string) template.HTML {
	parts := strings.Split(text, " → ")
	for i, p := range parts {
		parts[i] = template.HTMLEscapeString(p)
	}
	return template.HTML(strings.Join(parts, ` <span class="arrow">→</span> `))
}

var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{"arrows": arrows}).Parse(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
body{font:14px -apple-system,Helvetica,Arial,sans-serif;color:#222;max-width:820px;margin:24px auto;padding:0 16px}
h1{font-size:13px;color:#888;font-weight:normal;margin:0 0 20px}
h2{font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#666;border-bottom:1px solid #ddd;padding-bottom:4px;margin:28px 0 8px}
h2.needs{color:#b04a00;border-color:#f0c9a8}
h3{font-size:15px;margin:16px 0 6px}
p.note{margin:0 0 6px;color:#555}
table{border-collapse:collapse;width:100%;margin-bottom:6px}
td,th{padding:3px 10px 3px 0;text-align:left;vertical-align:top;white-space:nowrap}
th{font-size:12px;color:#888;font-weight:normal}
td.amt,th.amt{text-align:right;font-variant-numeric:tabular-nums}
td.wide{white-space:normal;width:100%}
.credit{color:#1a7f37}
.arrow{color:#999}
.muted{color:#888}
pre{background:#f6f6f6;padding:8px 10px;border-radius:4px;font-size:12px;margin:6px 0}
.box{background:#fff8f2;border:1px solid #f0d9c4;border-radius:6px;padding:4px 14px 10px;margin-bottom:10px}
</style></head><body>
<h1>{{.Title}}</h1>
{{- range .Notes}}
<p class="note">{{.}}</p>
{{- end}}
{{- range .Groups}}

<h2{{if .Needs}} class="needs"{{end}}>{{.Title}}</h2>
{{- range .Sections}}
{{if .Box}}<div class="box">
{{end}}<h3>{{.Title}}</h3>
{{- range .Parts}}
{{- if .Table}}
<table{{if .Table.Muted}} class="muted"{{end}}>
{{- if .Table.Head}}
<tr>{{range .Table.Head}}<th{{with .Class}} class="{{.}}"{{end}}>{{.Text}}</th>{{end}}</tr>
{{- end}}
{{- range .Table.Rows}}
<tr>{{range .}}<td{{with .Class}} class="{{.}}"{{end}}>{{arrows .Text}}</td>{{end}}</tr>
{{- end}}
</table>
{{- else if .Code}}
<pre>{{.Code}}</pre>
{{- else}}
<p class="note">{{.Line}}</p>
{{- end}}
{{- end}}
{{- if .Box}}
</div>
{{- end}}
{{- end}}
{{- end}}
</body></html>
`))
