package report

import (
	"io"
	"strings"
	"unicode/utf8"
)

// doc is a report laid out for the user: a header, notes on the whole run, and its groups.
type doc struct {
	header string
	notes  []string
	groups []group
}

// group is one of a report's parts, such as Needs You: its sections, left out when empty.
type group struct {
	title    string
	sections []section
}

// section is a titled list of parts.
type section struct {
	title string
	parts []part
}

// part is one piece of a section: a line, a table, or code to paste. A table or code after a
// line belongs to that line.
type part struct {
	line  string
	table *table
	code  []string
}

// table is rows under columns; the columns' names are its head, left out when all are empty.
type table struct {
	cols []column
	rows [][]string
}

// column is a table's column: its name and whether its cells, amounts, align right.
type column struct {
	name  string
	right bool
}

// cols are columns named names, left-aligned but for those named in right.
func cols(names []string, right ...string) []column {
	out := make([]column, len(names))
	for i, n := range names {
		out[i] = column{name: n}
		for _, r := range right {
			if n == r {
				out[i].right = true
			}
		}
	}
	return out
}

// add appends a group of the sections that aren't empty, unless all are.
func (d *doc) add(title string, sections ...section) {
	var g group
	for _, s := range sections {
		if len(s.parts) > 0 {
			g.sections = append(g.sections, s)
		}
	}
	if len(g.sections) > 0 {
		g.title = title
		d.groups = append(d.groups, g)
	}
}

// tabled is a section of one table, with no parts when the table has no rows.
func tabled(title string, t table) section {
	if len(t.rows) == 0 {
		return section{title: title}
	}
	return section{title: title, parts: []part{{table: &t}}}
}

// writeText prints d as plain text: group titles in capitals, rows indented, columns padded.
func (d doc) writeText(w io.Writer) error {
	p := &printer{w: w}
	p.f("%s\n", d.header)
	for _, n := range d.notes {
		p.f("%s\n", n)
	}
	for _, g := range d.groups {
		p.f("\n%s\n", strings.ToUpper(g.title))
		for _, s := range g.sections {
			p.f("\n%s\n", s.title)
			indent := "  "
			for _, pt := range s.parts {
				switch {
				case pt.table != nil:
					pt.table.writeText(p, indent)
				case pt.code != nil:
					for _, l := range pt.code {
						p.f("%s%s\n", indent, l)
					}
				default:
					p.f("  %s\n", pt.line)
					indent = "    "
				}
			}
		}
	}
	return p.err
}

// writeText prints t's head and rows, each indented, columns two spaces apart.
func (t table) writeText(p *printer, indent string) {
	rows := t.rows
	head := make([]string, len(t.cols))
	for i, c := range t.cols {
		head[i] = c.name
	}
	if strings.Join(head, "") != "" {
		rows = append([][]string{head}, rows...)
	}
	widths := make([]int, len(t.cols))
	for _, r := range rows {
		for i, cell := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, cell := range r {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if t.cols[i].right {
				cells[i] = pad + cell
			} else {
				cells[i] = cell + pad
			}
		}
		p.f("%s%s\n", indent, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
}
