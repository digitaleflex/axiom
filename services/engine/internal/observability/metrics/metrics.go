package metrics

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MaxLabelValueLen bounds the rune length of a single label value. Values
// longer than this are truncated and marked (see sanitizeLabelValue).
const MaxLabelValueLen = 64

// truncatedMarker is appended to a label value that exceeded MaxLabelValueLen.
const truncatedMarker = "…"

// allowedLabelKeys is the fixed set of label keys the Engine may use. Anything
// outside this set is rejected at registration (issue #103 acceptance:
// "metrics are stable, low-cardinality").
//
// Cardinality notes:
//
//	method   — bounded by the HTTP verb set (~10).
//	status   — bounded by the HTTP status codes the API can return.
//	state    — bounded by the deployment/runtime state enum.
//	step     — bounded by the plan step enum (BUILD, CREATE_RUNTIME, …).
//	code     — bounded by the stable error-code set.
//	server   — bounded by the number of registered servers; acceptable
//	           because it is operator-managed, not request-driven.
//	resource — bounded by {cpu, memory_mb, disk_free_mb}.
//
// "deploymentId", "requestId" and "correlationId" are absent on purpose:
// they are unbounded and must never be label values.
var allowedLabelKeys = map[string]struct{}{
	"method":   {},
	"status":   {},
	"state":    {},
	"step":     {},
	"code":     {},
	"server":   {},
	"resource": {},
}

// highCardinalityKeys are rejected with a dedicated message so the mistake is
// obvious at the call site.
var highCardinalityKeys = map[string]struct{}{
	"deploymentId":   {},
	"deploymentID":   {},
	"deployment_id":  {},
	"deployment":     {},
	"requestId":      {},
	"requestID":      {},
	"request_id":     {},
	"correlationId":  {},
	"correlationID":  {},
	"correlation_id": {},
	"path":           {},
	"url":            {},
	"query":          {},
}

// Metric kinds, used to detect duplicate names across kinds.
const (
	kindCounter   = "counter"
	kindGauge     = "gauge"
	kindHistogram = "histogram"
)

// DefaultDurationBuckets are the fixed upper bounds (seconds) used by the
// duration histograms. They are the conventional Prometheus buckets, covering
// sub-millisecond to ten-second operations.
var DefaultDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Registry owns every metric family registered with it. It is safe for
// concurrent use.
type Registry struct {
	mu         sync.Mutex
	kinds      map[string]string
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		kinds:      map[string]string{},
		counters:   map[string]*Counter{},
		gauges:     map[string]*Gauge{},
		histograms: map[string]*Histogram{},
	}
}

// Counter registers a new counter family. labelKeys declares the bounded label
// set; every key must be in the allow-list. It returns an error (never panics)
// for an invalid name, a duplicate name, or an unknown/high-cardinality label
// key.
func (r *Registry) Counter(name, help string, labelKeys ...string) (*Counter, error) {
	if err := r.validate(name, labelKeys); err != nil {
		return nil, err
	}
	c := &Counter{family: newFamily(name, help, labelKeys)}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds[name] = kindCounter
	r.counters[name] = c
	return c, nil
}

// Gauge registers a new gauge family. See Counter for validation rules.
func (r *Registry) Gauge(name, help string, labelKeys ...string) (*Gauge, error) {
	if err := r.validate(name, labelKeys); err != nil {
		return nil, err
	}
	g := &Gauge{family: newFamily(name, help, labelKeys)}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds[name] = kindGauge
	r.gauges[name] = g
	return g, nil
}

// Histogram registers a new fixed-bucket histogram family. buckets are the
// upper bounds in ascending order; when empty, DefaultDurationBuckets is used.
// The +Inf bucket is implicit. See Counter for validation rules.
func (r *Registry) Histogram(name, help string, buckets []float64, labelKeys ...string) (*Histogram, error) {
	if err := r.validate(name, labelKeys); err != nil {
		return nil, err
	}
	b, err := normalizeBuckets(buckets)
	if err != nil {
		return nil, err
	}
	h := &Histogram{family: newFamily(name, help, labelKeys), buckets: b}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds[name] = kindHistogram
	r.histograms[name] = h
	return h, nil
}

func (r *Registry) validate(name string, labelKeys []string) error {
	if !validMetricName(name) {
		return fmt.Errorf("metrics: invalid metric name %q", name)
	}
	if err := validateLabelKeys(labelKeys); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind, ok := r.kinds[name]; ok {
		return fmt.Errorf("metrics: metric %q already registered as %s", name, kind)
	}
	return nil
}

// validateLabelKeys enforces the cardinality guard at registration time.
func validateLabelKeys(keys []string) error {
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			return errors.New("metrics: empty label key")
		}
		if _, bad := highCardinalityKeys[k]; bad {
			return fmt.Errorf("metrics: label %q is a high-cardinality identifier and must not be a label; use a bounded dimension (method/status/state/step/code/server/resource) instead", k)
		}
		if _, ok := allowedLabelKeys[k]; !ok {
			return fmt.Errorf("metrics: unknown label key %q (allowed: %s)", k, allowedKeyList())
		}
		if _, dup := seen[k]; dup {
			return fmt.Errorf("metrics: duplicate label key %q", k)
		}
		seen[k] = struct{}{}
	}
	return nil
}

func allowedKeyList() string {
	keys := make([]string, 0, len(allowedLabelKeys))
	for k := range allowedLabelKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// validMetricName accepts the Prometheus metric-name grammar
// ^[a-zA-Z_:][a-zA-Z0-9_:]*$.
func validMetricName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == ':':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// sanitizeLabelValue caps a label value at MaxLabelValueLen runes, appending
// truncatedMarker when it had to be cut.
func sanitizeLabelValue(v string) string {
	runes := []rune(v)
	if len(runes) <= MaxLabelValueLen {
		return v
	}
	return string(runes[:MaxLabelValueLen-1]) + truncatedMarker
}

// family holds the shared state of one metric family.
type family struct {
	name      string
	help      string
	labelKeys []string
	mu        sync.Mutex
	samples   map[string]*sample
}

func newFamily(name, help string, labelKeys []string) family {
	return family{
		name:      name,
		help:      help,
		labelKeys: append([]string(nil), labelKeys...),
		samples:   map[string]*sample{},
	}
}

// sample is one time series.
type sample struct {
	labels map[string]string
	value  float64
	counts []uint64 // histogram per-bucket counts; len == len(buckets)+1
	sum    float64
}

// sampleLocked returns (creating if needed) the sample for labels. Callers must
// hold f.mu.
func (f *family) sampleLocked(labels map[string]string) *sample {
	key := f.encodeLabels(labels)
	s, ok := f.samples[key]
	if !ok {
		s = &sample{labels: f.boundedLabels(labels)}
		f.samples[key] = s
	}
	return s
}

// encodeLabels builds a collision-free map key from the declared label values
// in declaration order. Only declared keys are read: any extra key supplied by
// a caller is ignored, which is what keeps the series bounded.
func (f *family) encodeLabels(labels map[string]string) string {
	var b strings.Builder
	for _, k := range f.labelKeys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(sanitizeLabelValue(labels[k]))
		b.WriteByte(0)
	}
	return b.String()
}

// boundedLabels copies exactly the declared label keys, sanitized.
func (f *family) boundedLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(f.labelKeys))
	for _, k := range f.labelKeys {
		out[k] = sanitizeLabelValue(labels[k])
	}
	return out
}

// sortedSampleKeys returns the sample keys in deterministic order. Callers must
// hold f.mu.
func (f *family) sortedSampleKeys() []string {
	keys := make([]string, 0, len(f.samples))
	for k := range f.samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Counter is a monotonically increasing metric family.
type Counter struct{ family }

// Inc adds one to the series identified by labels.
func (c *Counter) Inc(labels map[string]string) { c.Add(labels, 1) }

// Add adds delta to the series identified by labels.
func (c *Counter) Add(labels map[string]string, delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sampleLocked(labels).value += delta
}

// Gauge is a metric family that can go up and down.
type Gauge struct{ family }

// Set replaces the value of the series identified by labels.
func (g *Gauge) Set(labels map[string]string, value float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sampleLocked(labels).value = value
}

// Add adds delta to the series identified by labels.
func (g *Gauge) Add(labels map[string]string, delta float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sampleLocked(labels).value += delta
}

// Histogram is a fixed-bucket histogram family. Buckets are cumulative in the
// exposition; storage keeps per-bucket counts and renders the prefix sum.
type Histogram struct {
	family
	buckets []float64 // ascending upper bounds, +Inf implicit
}

// Observe records value in the series identified by labels.
func (h *Histogram) Observe(labels map[string]string, value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sampleLocked(labels)
	if s.counts == nil {
		s.counts = make([]uint64, len(h.buckets)+1)
	}
	idx := len(h.buckets) // default: +Inf bucket
	for i, ub := range h.buckets {
		if value <= ub {
			idx = i
			break
		}
	}
	s.counts[idx]++
	s.sum += value
}

// normalizeBuckets validates and copies the injected buckets.
func normalizeBuckets(buckets []float64) ([]float64, error) {
	if len(buckets) == 0 {
		return append([]float64(nil), DefaultDurationBuckets...), nil
	}
	out := make([]float64, len(buckets))
	prev := 0.0
	for i, b := range buckets {
		if b != b { // NaN
			return nil, fmt.Errorf("metrics: histogram bucket %d is NaN", i)
		}
		if i == 0 {
			if b <= 0 {
				return nil, fmt.Errorf("metrics: histogram bucket %d must be positive", i)
			}
		} else if b <= prev {
			return nil, fmt.Errorf("metrics: histogram buckets must be strictly ascending (bucket %d: %v <= %v)", i, b, prev)
		}
		out[i] = b
		prev = b
	}
	return out, nil
}
