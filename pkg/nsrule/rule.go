// Package nsrule provides a mechanism for adding arbitrary tags to requests.
package nsrule

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/vm"
)

// RuleSet is a goroutine-safe container holding rules from a directory.
type RuleSet struct {
	rules atomic.Pointer[[]Rule]
}

// LoadFS loads rules from the provided filesystem in lexical order, replacing
// all existing ones. On error, the ruleset is left as-is.
func (s *RuleSet) LoadFS(fsys fs.FS) error {
	var rules []Rule
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			f, err := fsys.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()

			r, err := ParseRules(f, path.Clean(p))
			if err != nil {
				return fmt.Errorf("parse rules from %q: %w", p, err)
			}
			rules = append(rules, r...)
		}
		return nil
	}); err != nil {
		return err
	}
	s.rules.Store(&rules)
	return nil
}

// Evaluate evaluates r into t (which should not be nil) against e. The returned
// error list will almost always be nil since expressions are checked during
// parsing.
func (s *RuleSet) Evaluate(e Env, t Tags) []error {
	var errs []error
	if rs := s.rules.Load(); rs != nil {
		for _, r := range *rs {
			if err := r.Evaluate(e, t); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// Rule is a single rule consisting of an expression and tag mutations.
type Rule struct {
	name string
	line int
	expr *vm.Program
	muts []tagMut
}

// ParseRules parses rules from r, labeling them with name if provided.
//
// Each rule consists of an expression, continued on indented lines, followed by
// one or more further indented lines specifying tag mutations, like:
//
//	expression
//	 continued expression
//	 continued expression
//	  tag mutation
//	  tag mutation
//
// The exact amount and type of indentation doesn't matter, but has to be
// consistent within a rule. Blank lines or lines starting with # ignoring
// preceding whitespace are ignored.
//
// Expressions are checked for syntax errors and undefined names, but tag
// mutations are only checked for syntax errors.
func ParseRules(r io.Reader, name string) ([]Rule, error) {
	var (
		rs []Rule
		sc = bufio.NewScanner(r)

		line  string
		lineN int
		expB  strings.Builder
		expN  int
		muts  []string
		mutNs []int
		last  int // last indentation
		level int
	)
	for eof := false; !eof; {
	expLines:
		for {
			if !sc.Scan() {
				eof = true
				break expLines
			} else {
				line = sc.Text()
				lineN++
			}

			// ignore blank lines and comments
			if x := strings.TrimSpace(line); x == "" || strings.HasPrefix(x, "#") {
				continue
			}

			// determine indentation
			var indent int
			for _, x := range line {
				if !unicode.IsSpace(x) {
					break
				}
				indent++
			}

			// parse
			if indent == 0 {
				break expLines
			}
			if expB.Len() == 0 {
				return rs, fmt.Errorf("line %d: expected rule expression start, got indented line", lineN)
			}
			if indent > last {
				if level++; level > 2 {
					return rs, fmt.Errorf("line %d: too many indentation levels", lineN)
				}
				// we have another indent level, so tack the mutation lines onto
				// the expression
				for _, x := range muts {
					expB.WriteByte('\n')
					expB.WriteString(x)
				}
				muts = muts[:0]
				mutNs = mutNs[:0]
				last = indent
			}
			if indent != last {
				return rs, fmt.Errorf("line %d: unexpected de-indentation", lineN)
			}
			// we have another line at the current indent level, so assume
			// it's a mutation
			muts = append(muts, line)
			mutNs = append(mutNs, lineN)
		}

		// process the pending rule
		if expB.Len() != 0 {

			// ensure the rule is complete
			if len(muts) == 0 {
				return rs, fmt.Errorf("line %d: expected rule (expression %q) to contain tag mutations", lineN, expB.String())
			}

			// compile the rule
			r := Rule{
				name: name,
				line: expN,
			}
			if v, err := expr.Compile(expB.String(), append([]expr.Option{expr.AsBool(), expr.Optimize(true), expr.Env(dummyEnv)}, extraOptions...)...); err != nil { // TODO: dummy env
				return rs, fmt.Errorf("line %d: compile rule expression: %w", expN, err)
			} else {
				r.expr = v
			}
			r.muts = make([]tagMut, len(muts))
			for i := range r.muts {
				if v, err := parseTagMut(muts[i]); err != nil {
					return rs, fmt.Errorf("line %d: parse tag mutation: %w", mutNs[i], err)
				} else {
					r.muts[i] = v
				}
			}
			rs = append(rs, r)

			// clear the rule state
			expB.Reset()
			expN = 0
			muts = muts[:0]
			mutNs = mutNs[:0]
			last = 0
			level = 0
		}

		// start the new rule
		if !eof {
			expB.WriteString(line)
			expN = lineN
		}
	}
	return rs, sc.Err()
}

// Evaluate evaluates r into t (which should not be nil) against e. The returned
// error will almost always be nil since expressions are checked during parsing.
func (r Rule) Evaluate(e Env, t Tags) error {
	v, err := expr.Run(r.expr, e)
	if err != nil {
		return fmt.Errorf("evaluate rule at %s:%d: %w", r.name, r.line, err)
	}
	if v.(bool) {
		if t != nil {
			for _, m := range r.muts {
				m.Apply(t)
			}
		}
	}
	return nil
}
