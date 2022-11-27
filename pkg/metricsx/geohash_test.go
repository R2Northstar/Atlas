package metricsx

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/VictoriaMetrics/metrics"
)

func TestGeoCounter(t *testing.T) {
	var exp string
	if d, err := gzip.NewReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(``+
		`H4sIAAAAAAAA/4zZSW7lVACG0TmriN6YQZzndlCLwb1s2bjvUPaOEPPKmV9dquDg/8vNVq3bP2XdlHXz67WW9VrWrz+b6u/2r7X99Xp9f3z+8fsjn5+v74/4p0PB6/vj/dOhr9f3x9dPh96v74/gp0OR/OMSuSmVP1MmN+VyUyE3VXKokUOd/Hsa5KZJblrkpk0OHXLo`+
		`kkMPHAo+4b9dEMhNYjwQ40Ekh8R4IMYDMR6I8UCMB2I8EONBJ4fEeDDJITEeiPFAjAdiPBDjX5/wf/DXl9wUyk2x3JTKoVwOlXKolkOt/O16uWmUm2a5aZVDuxw65dANh96fckg8vUM5JJ7e4uktnt7i6S2e3q0cEk/vUQ6Jp7d4eount3h6i6fwP0/hT4cCGKBQNjiU`+
		`DQ4juUk2OJQNDmWDQ9ngUDY4lA0OZYPDTv5MssHhJDfJBoeywaFscCgbHMoGR9KZkXRmJMYjMR5JZ0ZiPBLjkRiPxHgkxiMxHonxSDozEuORdGYkxiMxHonxSIxHYjwW47F0QRzKTdIFsXRBLF0QSxfE0gVxK3876YJ4lJukC2Lpgli6IJYuiKULEunMRDwl0pmJeErE`+
		`UyKeEvGUiKdEOjMRT4l0ZiKeEvGUiKdEPCXiKZWfg1N5z0wFXSobnMp7ZiobnIrMVDY4Fb6pbHAqG5zKBqfynpnKBqfynpnKBqeywalscCobnMoGZ/LNzKQzMzGeifFMOjMT45kYz8R4JsYzMZ6J8UyMZ9KZmRjPpDMzMZ6J8UyMZ2I8E+O5fMdz4ZvLe2YuXZCLzFzQ`+
		`5dIFuXRBLu+ZuXRBLu+ZuXRBLl2QSxfk0gW5dEEh38xCPBXSmYV4KsRTIZ4K8VSIp0I6sxBPhXRmIZ4K8VSIp0I8FeKplJ+DS3nPLAVdKRtcyntmKRtcisxSNrgUvqVscCkbXMoGl/KeWcoGl/KeWcoGl7LBpWxwKRtcygZX8s2spDMrMV6J8Uo6sxLjlRivxHglxisx`+
		`XonxSoxX0pmVGK+kMysxXonxSoxXYrwS47V8x2vhW8t7Zi1dUIvMWtDV0gW1dEEt75m1dEEt75m1dEEtXVBLF9TSBbV0QSPfzEY8NdKZjXhqxFMjnhrx1IinRjqzEU+NdGYjnhrx1IinRjw14qmV35u30pmt/E6xlQ1upTNb2eBWfqfYyga38jvFVja4lQ1uZYNb6cxW`+
		`NriVzmxlg1vZ4FY2uJUNbmWDO9ngTjqzE+OdGO+kMzsx3onxTox3YrwT450Y78R4J53ZifFOOrMT450Y78R4J8Y7Md6L8V66oJfO7KULeumCXrqgly7opQt66cxeuqCXzuylC3rpgl66oJcu6KULBunMQTwN0pmDeBrE0yCeBvE0iKdBOnMQT4N05iCeBvE0iKdBPA3i`+
		`aZTOHKUzR9ngUTZ4lM4cZYNH2eBRNniUDR5lg0fZ4FE2eJTOHGWDR+nMUTZ4lA0eZYNH2eBRNniSDZ6kMycxPonxSTpzEuOTGJ/E+CTGJzE+ifFJjE/SmZMYn6QzJzE+ifFJjE9ifBLjsxifpQtm6cxZumCWLpilC2bpglm6YJbOnKULZunMWbpgli6YpQtm6YJZumCR`+
		`zlzE0yKduYinRTwt4mkRT4t4WqQzF/G0SGcu4mkRT4t4WsTTIp5W+T6t0pmroFtlg1fpzFU2eBWZq2zwKnxX2eBVNniVDV6lM1fZ4FU6c5UNXmWDV9ngVTZ4lQ3e5Ju5SWduYnwT45t05ibGNzG+ifFNjG9ifBPjmxjfpDM3Mb5JZ25ifBPjmxjfxPgmxnf5ju/Cd5fO`+
		`3KULdpG5C7pdumCXLtilM3fpgl06c5cu2KULdumCXbpgly445Jt5iKdDOvMQT4d4OsTTIZ4O8XRIZx7i6ZDOPMTTIZ4O8XSIp0M8nfJ9OqUzT0F3ygaf0pmnbPApMk/Z4FP4nrLBp2zwKRt8SmeessGndOYpG3zKBp+ywads8CkbfMk385LOvMT4JcYv6cxLjF9i/BLj`+
		`lxi/xPglxi8xfklnXmL8ks68xPglxi8xfonxS4zf8h2/he8tnXlLF9wi8xZ0t3TBLV1wS2fe0gW3dOYtXXBLF9zSBbd0wS1d8Mg38xFPj3TmI54e8fSIp0c8PeLpkc58xNMjnfmIp0c8PeLpEU/P/57+DQAA//8lgllOY1MAAA==`,
	))); err != nil {
		panic(err)
	} else if x, err := io.ReadAll(d); err != nil {
		panic(err)
	} else {
		exp = strings.TrimSpace(string(x))
	}

	set := metrics.NewSet()
	name := `test{dfgdfg="sdfsdf"}`
	gc1 := NewGeoCounter(set, name, 2)
	gc2 := NewGeoCounter2(name)

	for lat := float64(-90); lat <= 90; lat += 10 {
		for lng := float64(-180); lng <= 180; lng += 10 {
			gc1.Inc(lat, lng)
			gc2.Inc(lat, lng)
		}
	}

	var b1 strings.Builder
	set.WritePrometheus(&b1)

	var b2 strings.Builder
	gc2.WritePrometheus(&b2)

	t.Run("GeoCounter", func(t *testing.T) {
		if a := strings.TrimSpace(b1.String()); a != exp {
			t.Errorf("expected:\n\t%s\n, got\n\t%s", strings.ReplaceAll(exp, "\n", "\n\t"), strings.ReplaceAll(a, "\n", "\n\t"))
		}
	})

	t.Run("GeoCounter2", func(t *testing.T) {
		if a := strings.TrimSpace(b2.String()); a != exp {
			t.Errorf("expected:\n\t%s\n, got\n\t%s", strings.ReplaceAll(exp, "\n", "\n\t"), strings.ReplaceAll(a, "\n", "\n\t"))
		}
	})
}

func BenchmarkGeoCounter2(b *testing.B) {
	var pts [][2]float64
	for lat := float64(-90); lat <= 90; lat += 10 {
		for lng := float64(-180); lng <= 180; lng += 10 {
			pts = append(pts, [2]float64{lat, lng})
		}
	}

	b.Run("GeoCounter", func(b *testing.B) {
		set := metrics.NewSet()
		ctr := NewGeoCounter(set, `test{dfgdfg="sdfsdf"}`, 2)

		b.Run("Inc", func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				pt := pts[n%len(pts)]
				ctr.Inc(pt[0], pt[1])
			}
		})

		b.Run("WritePrometheus", func(b *testing.B) {
			var buf bytes.Buffer
			set.WritePrometheus(&buf)
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				buf.Reset()
				set.WritePrometheus(&buf)
			}
		})
	})

	b.Run("GeoCounter2", func(b *testing.B) {
		ctr := NewGeoCounter2(`test{dfgdfg="sdfsdf"}`)

		b.Run("Inc", func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				pt := pts[n%len(pts)]
				ctr.Inc(pt[0], pt[1])
			}
		})

		b.Run("WritePrometheus", func(b *testing.B) {
			var buf bytes.Buffer
			ctr.WritePrometheus(&buf)
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				buf.Reset()
				ctr.WritePrometheus(&buf)
			}
		})
	})
}
