// Package metricsx extends github.com/VictoriaMetrics/metrics.
package metricsx

import "strings"

func splitName(name string) (base, arg string) {
	if n := len(name); n != 0 {
		base = name
		for i, r := range base {
			if r == '{' {
				if j := len(base) - 1; j > i && base[j] == '}' {
					base, arg = base[:i], base[i+1:j]
					break
				}
			}
		}
	}
	return
}

func formatName(base, arg string, args ...string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteByte('{')
	if arg != "" {
		b.WriteString(arg)
	}
	for i := 1; i < len(args); i += 2 {
		if arg != "" || i > 1 {
			b.WriteByte(',')
		}
		b.WriteString(args[i-1])
		b.WriteString("=\"")
		b.WriteString(args[i])
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}
