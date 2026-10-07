package metrics

import (
	"io"
	"sort"
	"strconv"
	"strings"
)

// ContentType is the Prometheus text exposition content type.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// familyView is a rendered, immutable view of one metric family.
type familyView struct {
	name  string
	help  string
	typ   string
	lines []string
}

// WriteText writes the registry in Prometheus text exposition format. Output is
// deterministic: families are sorted by name and, within a family, samples are
// sorted by their encoded label set. It is safe to call concurrently with
// metric updates.
func (r *Registry) WriteText(w io.Writer) error {
	var b strings.Builder
	for _, v := range r.gather() {
		b.WriteString("# HELP ")
		b.WriteString(v.name)
		b.WriteByte(' ')
		b.WriteString(escapeHelp(v.help))
		b.WriteByte('\n')
		b.WriteString("# TYPE ")
		b.WriteString(v.name)
		b.WriteByte(' ')
		b.WriteString(v.typ)
		b.WriteByte('\n')
		for _, line := range v.lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Text returns the exposition snapshot as a string.
func (r *Registry) Text() string {
	var b strings.Builder
	_ = r.WriteText(&b)
	return b.String()
}

// gather renders every family. It copies under the registry lock and renders
// under each family lock; registration only ever takes the registry lock and
// metric updates only ever take a family lock, so the ordering is acyclic.
func (r *Registry) gather() []familyView {
	r.mu.Lock()
	views := make([]familyView, 0, len(r.kinds))
	for name, kind := range r.kinds {
		switch kind {
		case kindCounter:
			views = append(views, r.counters[name].view(kindCounter))
		case kindGauge:
			views = append(views, r.gauges[name].view(kindGauge))
		case kindHistogram:
			views = append(views, r.histograms[name].viewHistogram())
		}
	}
	r.mu.Unlock()
	sort.Slice(views, func(i, j int) bool { return views[i].name < views[j].name })
	return views
}

// view renders a counter or gauge family.
func (f *family) view(typ string) familyView {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := f.sortedSampleKeys()
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		s := f.samples[k]
		lines = append(lines, sampleLine(f.name, f.labelKeys, s.labels, "", formatFloat(s.value)))
	}
	return familyView{name: f.name, help: f.help, typ: typ, lines: lines}
}

// viewHistogram renders a histogram family with cumulative buckets, _sum and
// _count.
func (h *Histogram) viewHistogram() familyView {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := h.sortedSampleKeys()
	lines := make([]string, 0, len(keys)*(len(h.buckets)+3))
	for _, k := range keys {
		s := h.samples[k]
		var cumulative uint64
		for i, ub := range h.buckets {
			cumulative += s.counts[i]
			lines = append(lines, sampleLine(h.name+"_bucket", h.labelKeys, s.labels, formatFloat(ub), formatUint(cumulative)))
		}
		cumulative += s.counts[len(h.buckets)]
		lines = append(lines, sampleLine(h.name+"_bucket", h.labelKeys, s.labels, "+Inf", formatUint(cumulative)))
		lines = append(lines, sampleLine(h.name+"_sum", h.labelKeys, s.labels, "", formatFloat(s.sum)))
		lines = append(lines, sampleLine(h.name+"_count", h.labelKeys, s.labels, "", formatUint(cumulative)))
	}
	return familyView{name: h.name, help: h.help, typ: kindHistogram, lines: lines}
}

// sampleLine builds one exposition line: name{k="v",le="x"} value. The le
// label, when present, always comes last.
func sampleLine(name string, labelKeys []string, labels map[string]string, le, value string) string {
	var b strings.Builder
	b.WriteString(name)
	if len(labelKeys) > 0 || le != "" {
		b.WriteByte('{')
		first := true
		for _, k := range labelKeys {
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(k)
			b.WriteString(`="`)
			b.WriteString(escapeLabelValue(labels[k]))
			b.WriteByte('"')
		}
		if le != "" {
			if !first {
				b.WriteByte(',')
			}
			b.WriteString(`le="`)
			b.WriteString(le)
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	return b.String()
}

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func formatUint(v uint64) string { return strconv.FormatUint(v, 10) }

// escapeLabelValue escapes the three characters Prometheus requires in a label
// value: backslash, double quote and newline.
func escapeLabelValue(s string) string {
	if !strings.ContainsAny(s, "\\\"\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeHelp escapes backslash and newline in a HELP string.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}
