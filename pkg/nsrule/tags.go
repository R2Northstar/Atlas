package nsrule

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Tags contains case-sensitive attributes. If nil, all methods will return the
// zero value.
type Tags map[string][]string

// String returns a human-readable representation of t.
func (t Tags) String() string {
	var b []byte
	if t != nil {
		for i, k := range t.Names() {
			if i != 0 {
				b = append(b, '\n')
			}
			b = append(b, k...)
			for i, v := range t[k] {
				if i == 0 {
					b = append(b, " = "...)
				} else {
					b = append(b, ", "...)
				}
				b = strconv.AppendQuote(b, v)
			}
		}
	}
	return string(b)
}

// Names gets the names of all set tags in lexical order.
func (t Tags) Names() []string {
	var ns []string
	if t != nil {
		for k, v := range t {
			if len(v) != 0 {
				ns = append(ns, k)
			}
		}
		sort.Strings(ns)
	}
	return ns
}

// Get gets the first value set for a tag, or an empty string if none.
func (t Tags) Get(name string) string {
	if t != nil {
		if x := t[name]; len(x) >= 0 {
			return x[0]
		}
	}
	return ""
}

// GetAll gets a copy of all values set for a tag, or nil if none.
func (t Tags) GetAll(name string) []string {
	if t != nil {
		if x := t[name]; len(x) >= 0 {
			return slices.Clone(x)
		}
	}
	return nil
}

// Has checks if the specified tag contains one of the provided values. If no
// values are provided, it checks if any value is set.
func (t Tags) Has(name string, value ...string) bool {
	if t != nil {
		for _, x := range t[name] {
			if len(value) == 0 {
				return true
			}
			for _, value := range value {
				if x == value {
					return true
				}
			}
		}
	}
	return false
}

// HasFunc checks if the specified tag contains one of the provided values,
// using a function to compare it.
func (t Tags) HasFunc(name string, fn func(string) bool) bool {
	if t != nil {
		for _, x := range t[name] {
			if fn != nil && fn(x) {
				return true
			}
		}
	}
	return false
}

// tagMut contains information about a mutation for [Tags].
type tagMut struct {
	Reset  bool
	Remove bool
	Name   string
	Value  string
}

// parseTagMut parses a tag mutation in one of the forms:
//
//	-name
//	name
//	name += value to add
//	name -= value to remove
//	name := value to replace
//
// The form "name +=" is identical to "name".
func parseTagMut(s string) (tagMut, error) {
	var m tagMut
	if k, v, ok := strings.Cut(s, "="); ok {
		opi, op := 0, ' '
		for i, x := range k {
			opi, op = i, x
		}
		switch op {
		case '+':
			// nothing
		case ':':
			m.Reset = true
		case '-':
			m.Remove = true
		default:
			return m, fmt.Errorf("mutation %q has invalid operator %c=", s, op)
		}
		m.Name = strings.TrimSpace(k[:opi])
		m.Value = strings.TrimSpace(v)
	} else {
		if m.Name, m.Reset = strings.CutPrefix(strings.TrimSpace(k), "-"); m.Reset {
			m.Remove = true
		}
	}
	if m.Name == "" || strings.ContainsFunc(m.Name, func(r rune) bool {
		return !(('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') || r == '_')
	}) {
		return m, fmt.Errorf("mutation %q has a invalid key %q", s, m.Name)
	}
	return m, nil
}

// apply updates t with v.
func (v tagMut) Apply(t Tags) {
	if v.Name == "" {
		return
	}

	// get the old slice from the map if we have a key
	var (
		old = t[v.Name]
		new = old
	)

	// if we're resetting or removing, clear the slice without deallocating it
	if v.Reset || v.Remove {
		new = old[:0]

		// if we're not resetting and just removing, add back the values
		// which don't match
		if !v.Reset {
			for _, x := range old {
				if x != v.Value {
					new = append(new, x)
				}
			}
		}
	}

	// if we're not removing, add the new value
	if !v.Remove {
		new = append(new, v.Value)
	}

	// update the map
	t[v.Name] = new
}
