package metricsx

import (
	"io"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/VictoriaMetrics/metrics"
	"github.com/mmcloughlin/geohash"
)

// GeoCounter is like a *metrics.Counter, but split by location using geohashes.
type GeoCounter struct {
	level uint
	ctr   []*metrics.Counter
	unk   *metrics.Counter
	set   *metrics.Set
	base  string
	arg   string
}

// NewGeoCounter creates a new GeoCounter writing to metrics in set named name,
// with level chars in the geohash.
func NewGeoCounter(set *metrics.Set, name string, level uint) *GeoCounter {
	if h, p := geohash.ConvertStringToInt(strings.Repeat("z", int(level))); h != 1<<(5*level)-1 || p != 5*uint(level) {
		panic("wtf") // this shouldn't happen... geohashes are base32, and int encoding is 5 bits per char
	}
	base, arg := splitName(name)
	return &GeoCounter{
		level: level,
		ctr:   make([]*metrics.Counter, 1<<(5*level)),
		unk:   set.NewCounter(formatName(base, arg, "geohash", "")),
		set:   set,
		base:  base,
		arg:   arg,
	}
}

// Inc increments the counter for the specified latitude and longitude.
func (c *GeoCounter) Inc(lat, lng float64) {
	c.Counter(lat, lng).Inc()
}

// Set sets the counter for the specified latitude and longitude.
func (c *GeoCounter) Set(lat, lng float64, v uint64) {
	c.Counter(lat, lng).Set(v)
}

// IncUnknown increments the unknown counter.
func (c *GeoCounter) IncUnknown() {
	c.unk.Inc()
}

// SetUnknown sets the unknown counter.
func (c *GeoCounter) SetUnknown(v uint64) {
	c.unk.Set(v)
}

// Counter gets the underlying counter for the specified latitude and longitude.
func (c *GeoCounter) Counter(lat, lng float64) *metrics.Counter {
	h := geohash.EncodeIntWithPrecision(lat, lng, c.level*5)
	if int(h) >= len(c.ctr) {
		return nil // wtf (this shouldn't even be possible, but we don't panic here for performance reasons)
	}
	m := c.ctr[h]
	if m == nil {
		m = c.set.NewCounter(formatName(c.base, c.arg, "geohash", geohash.EncodeWithPrecision(lat, lng, c.level)))
		c.ctr[h] = m
	}
	return m
}

// CounterUnknown gets the underlying counter for unknown positions.
func (c *GeoCounter) CounterUnknown() *metrics.Counter {
	return c.unk
}

// GeoCounter2 is an optimized standalone level 2 geocounter metric. It must not
// be copied (it uses atomics).
type GeoCounter2 struct {
	name string
	ctr  [1 << (5 * 2)]uint64
	unk  uint64
}

// NewGeoCounter2 creates a new GeoCounter2 with the provided metric name.
//
// Note: The maximum cardinality of metrics produced will be 1024.
func NewGeoCounter2(name string) *GeoCounter2 {
	b, a := splitName(name)
	n := formatName(b, a, "geohash", "")
	if !strings.HasSuffix(n, `geohash=""}`) {
		panic("wtf") // should never happen
	}
	return &GeoCounter2{name: n}
}

// Inc increments the counter for the specified latitude and longitude.
func (c *GeoCounter2) Inc(lat, lng float64) {
	if c != nil {
		// this should always be true, but we need it to satisfy the bounds checker
		if h := geohash2(lat, lng); h < 1<<(5*2) {
			atomic.AddUint64(&c.ctr[h], 1)
		}
	}
}

// Set sets the counter for the specified latitude and longitude.
func (c *GeoCounter2) Set(lat, lng float64, v uint64) {
	if c != nil {
		// this should always be true, but we need it to satisfy the bounds checker
		if h := geohash2(lat, lng); h < 1<<(5*2) {
			atomic.StoreUint64(&c.ctr[h], 1)
		}
	}
}

// IncUnknown increments the unknown counter.
func (c *GeoCounter2) IncUnknown() {
	atomic.AddUint64(&c.unk, 1)
}

// SetUnknown sets the unknown counter.
func (c *GeoCounter2) SetUnknown(v uint64) {
	atomic.StoreUint64(&c.unk, v)
}

// WritePrometheus writes the Promethus text metrics.
func (c *GeoCounter2) WritePrometheus(w io.Writer) {
	n := len(c.name)
	b := make([]byte, 0, n+2+1+20+1)
	b = append(b, c.name...)
	w.Write(append(strconv.AppendUint(append(b, ' '), atomic.LoadUint64(&c.unk), 10), '\n'))
	b = append(b, `"} `...)
	_ = b[n-2] // bounds check hint
	for h := uint64(0); h < 1<<(5*2); h++ {
		if v := atomic.LoadUint64(&c.ctr[h]); v != 0 {
			b[n-1] = "0123456789bcdefghjkmnpqrstuvwxyz"[(h>>0)&0x1f]
			b[n-2] = "0123456789bcdefghjkmnpqrstuvwxyz"[(h>>5)&0x1f]
			w.Write(append(strconv.AppendUint(b, v, 10), '\n'))
		}
	}
}

func geohash2(lat, lng float64) uint64 {
	return geohash.EncodeIntWithPrecision(lat, lng, 5*2)
}
