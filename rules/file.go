package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// File is the rules file at Path, its categories resolved against Categories.
type File struct {
	Path       string
	Categories []ledger.Category
}

// Edit sets the category, state and start date of the rule at Index, counted from 0 in file
// order, to Rule's; an empty state or a zero start date removes the field.
type Edit struct {
	Index int
	Rule  Rule
}

// Addition is a rule appended to the file under a comment line: its payee, category and state.
type Addition struct {
	Comment string
	Rule    Rule
}

// Revise writes edits and additions into the file, leaving every other line as it was, and
// returns the rules it then holds. The file is replaced, keeping its mode, only once the new
// text loads.
func (f File) Revise(edits []Edit, additions []Addition) (Rules, error) {
	old, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		return nil, err
	}
	text, err := revised(string(old), edits, additions)
	if err != nil {
		return nil, fmt.Errorf("rules %s: %w", f.Path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".rules-*.toml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return nil, err
	}
	rs, err := Load(tmp.Name(), f.Categories)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), f.Path); err != nil {
		return nil, err
	}
	return rs, nil
}

var header = regexp.MustCompile(`^\s*\[\[rule\]\]\s*(#.*)?$`)

// revised is text with edits made and additions appended.
func revised(text string, edits []Edit, additions []Addition) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, e := range edits {
		var starts []int
		for i, l := range lines {
			if header.MatchString(l) {
				starts = append(starts, i)
			}
		}
		if e.Index < 0 || e.Index >= len(starts) {
			return "", fmt.Errorf("no rule %d", e.Index+1)
		}
		end := len(lines)
		if e.Index+1 < len(starts) {
			end = starts[e.Index+1]
		}
		block := slices.Clone(lines[starts[e.Index]+1 : end])
		block = set(block, "category", quote(e.Rule.Category))
		block = set(block, "state", optional(e.Rule.State != "", quote(e.Rule.State)))
		block = set(block, "from", optional(!e.Rule.From.IsZero(), day.Format(e.Rule.From)))
		lines = slices.Concat(lines[:starts[e.Index]+1], block, lines[end:])
	}
	out := strings.Join(lines, "")
	if len(additions) > 0 && out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	for _, a := range additions {
		out += "\n# " + a.Comment + "\n[[rule]]\npayee = " + literal(a.Rule.Payee) + "\ncategory = " + quote(a.Rule.Category) + "\n"
		if a.Rule.State != "" {
			out += "state = " + quote(a.Rule.State) + "\n"
		}
	}
	return out, nil
}

// optional is value when present, else empty, which removes the field.
func optional(present bool, value string) string {
	if !present {
		return ""
	}
	return value
}

// set gives field value in a rule's block, rewriting the line that holds it with the block's
// indentation, adding it after the block's last field when it has none, or removing it when
// value is empty.
func set(block []string, field, value string) []string {
	re := regexp.MustCompile(`^(\s*)` + field + `\s*=`)
	last, indent := -1, ""
	for i, l := range block {
		if m := re.FindStringSubmatch(l); m != nil {
			if value == "" {
				return slices.Delete(block, i, i+1)
			}
			block[i] = m[1] + field + " = " + value + "\n"
			return block
		}
		if m := fieldLine.FindStringSubmatch(l); m != nil {
			last, indent = i, m[1]
		}
	}
	if value == "" {
		return block
	}
	if last >= 0 && !strings.HasSuffix(block[last], "\n") {
		block[last] += "\n"
	}
	return slices.Insert(block, last+1, indent+field+" = "+value+"\n")
}

var fieldLine = regexp.MustCompile(`^(\s*)[A-Za-z_]+\s*=`)

// quote is s as a TOML basic string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// literal is s as a TOML literal string, which keeps a pattern's backslashes as written, or a
// basic string when s holds a quote.
func literal(s string) string {
	if strings.ContainsAny(s, "'\n") {
		return quote(s)
	}
	return "'" + s + "'"
}
