package metricsx

import "testing"

func TestSplitName(t *testing.T) {
	for _, c := range [][3]string{
		// valid
		{`test`, `test`, ``},
		{`test{}`, `test`, ``},
		{`test{test=""}`, `test`, `test=""`},
		{`test{test="{}"}`, `test`, `test="{}"`},

		// invalid
		{``, ``, ``},
		{`test{`, `test{`, ``},
		{`test}`, `test}`, ``},
		{`test}{`, `test}{`, ``},
		{`test{}{}`, `test`, `}{`},
		{`test{}{test}`, `test`, `}{test`},
		{`test{test{}}`, `test`, `test{}`},
		{`test{}{test{}}`, `test`, `}{test{}`},
	} {
		name, xbase, xarg := c[0], c[1], c[2]
		if base, arg := splitName(name); base != xbase || arg != xarg {
			t.Errorf("split %#q: expected (%#q, %#q), got (%#q, %#q)", name, xbase, xarg, base, arg)
		}
	}
}

func TestFormatName(t *testing.T) {
	for _, c := range [][]string{
		{`test{}`, `test`, ``},
		{`test{a="1"}`, `test`, `a="1"`},
		{`test{a="1",b="2"}`, `test`, `a="1"`, `b`, `2`},
		{`test{a="1",b="2"}`, `test`, `a="1",b="2"`},
		{`test{a="1",b="2",c="3"}`, `test`, `a="1"`, `b`, `2`, `c`, `3`},
		{`test{a="1",b="2",c="3"}`, `test`, `a="1",b="2"`, `c`, `3`},
	} {
		exp, base, arg, args := c[0], c[1], c[2], c[3:]
		if act := formatName(base, arg, args...); act != exp {
			t.Errorf("format (%#q, %#q, %#q, %#q): expected %#q, got %#q", exp, base, arg, args, exp, act)
		}
	}
}
