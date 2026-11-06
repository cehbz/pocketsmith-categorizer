// Package leak finds words from the user's real payees in text bound for a public repository.
package leak

import (
	"slices"
	"strings"
	"unicode"
)

const (
	minLetters = 4 // a shorter word doesn't identify anyone and is often a common abbreviation
	minDigits  = 7 // a shorter number is a store number or part of a date
)

// inflections are the endings removed to recognize an ordinary word, which a dictionary lists
// only in its base form.
var inflections = []string{"S", "ES", "D", "ED", "ING", "R", "ER", "ERS"}

// Private is the words of real payees that could identify someone.
type Private map[string]bool

// NewPrivate keeps the words of payees that aren't among the ordinary words.
func NewPrivate(payees, ordinary []string) Private {
	dictionary := map[string]bool{}
	for _, o := range ordinary {
		dictionary[strings.ToUpper(o)] = true
	}
	p := Private{}
	for _, payee := range payees {
		for _, w := range words(payee) {
			if identifying(w) && !isOrdinary(w, dictionary) {
				p[w] = true
			}
		}
	}
	return p
}

// In returns, sorted, the private words in text that known doesn't already use.
func (p Private) In(text, known string) []string {
	used := map[string]bool{}
	for _, w := range words(known) {
		used[w] = true
	}
	var found []string
	for _, w := range words(text) {
		if p[w] && !used[w] && !slices.Contains(found, w) {
			found = append(found, w)
		}
	}
	slices.Sort(found)
	return found
}

// words splits s into upper-case runs of letters, digits and the marks of other scripts.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
}

func identifying(w string) bool {
	if strings.IndexFunc(w, unicode.IsLetter) >= 0 {
		return len([]rune(w)) >= minLetters
	}
	return len(w) >= minDigits
}

func isOrdinary(w string, dictionary map[string]bool) bool {
	if dictionary[w] {
		return true
	}
	return slices.ContainsFunc(inflections, func(ending string) bool {
		base, ok := strings.CutSuffix(w, ending)
		return ok && dictionary[base]
	})
}
