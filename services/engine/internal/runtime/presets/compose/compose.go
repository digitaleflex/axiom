// Package compose provides the bounded Docker Compose preset (issue #123):
// parsing a Compose document, validating it against Axiom's security policy,
// selecting the services an application deploys, computing their dependency
// order, and producing the per-service build plan Axiom needs to build images.
//
// Scope (#123): Compose discovery, service selection, dependency-graph
// validation, network boundary, volume policy and allowed capabilities. The
// package never talks to Docker and never executes anything: it is pure
// parsing and validation so it is safe to call from the analyzer, profile,
// planner and build layers.
//
// Design decisions (documented per #123):
//
//   - Axiom deploys a *bounded* subset of Compose. Host control is refused:
//     sensitive host bind mounts, network_mode/pid/ipc: host, privileged: true
//     and privileged host ports are hard errors. volumes_from is a warning.
//   - Service selection is closed under depends_on: a service cannot start
//     without its dependencies, so selecting one transitively selects them.
//     The dependency order is deterministic (Kahn's algorithm, name-sorted).
//   - Validation rules carry stable codes so callers branch on the code,
//     never on the message.
//   - Unknown Compose keys are preserved by the parser where they do not
//     affect the security decision; {@link Rewrite} mutates the original YAML
//     node tree so a rewritten document never silently drops user config.
package compose

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrParse is returned when the content is not a valid Compose document.
var ErrParse = errors.New("compose: invalid document")

// Document is the subset of a Compose document Axiom models. The maps are
// keyed by name; each {@link Service} carries its own name so consumers do not
// have to thread the key through.
type Document struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
	Networks map[string]Network `yaml:"networks"`
	Volumes  map[string]Volume  `yaml:"volumes"`
}

// Network is a top-level network definition.
type Network struct {
	Driver   string `yaml:"driver"`
	External bool   `yaml:"external"`
}

// Volume is a top-level volume definition.
type Volume struct {
	Driver   string `yaml:"driver"`
	External bool   `yaml:"external"`
}

// Service is one Compose service. Only the fields Axiom needs to validate the
// deployment boundary and to build images are modelled; everything else is
// preserved by {@link Rewrite} because it operates on the raw YAML tree.
type Service struct {
	Name        string     `yaml:"-"`
	Build       *Build     `yaml:"build"`
	Image       string     `yaml:"image"`
	Ports       []Port     `yaml:"ports"`
	DependsOn   StringList `yaml:"depends_on"`
	Volumes     []string   `yaml:"volumes"`
	Environment EnvList    `yaml:"environment"`
	Networks    StringList `yaml:"networks"`
	NetworkMode string     `yaml:"network_mode"`
	PID         string     `yaml:"pid"`
	IPC         string     `yaml:"ipc"`
	Privileged  bool       `yaml:"privileged"`
	VolumesFrom []string   `yaml:"volumes_from"`
}

// Build is a service build definition. Compose accepts either the scalar
// shorthand (`build: .`) or the mapping form (`build: {context: ., dockerfile: Dockerfile}`).
type Build struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// UnmarshalYAML accepts both the scalar and the mapping build shorthand.
func (b *Build) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		b.Context = strings.TrimSpace(value.Value)
		return nil
	case yaml.MappingNode:
		type plain Build
		var p plain
		if err := value.Decode(&p); err != nil {
			return err
		}
		*b = Build(p)
		return nil
	default:
		return fmt.Errorf("%w: build must be a string or a mapping", ErrParse)
	}
}

// StringList accepts the list form and the mapping-key form Compose uses for
// depends_on, networks and similar keys. Mapping keys are sorted so the result
// is deterministic.
type StringList []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if v := strings.TrimSpace(value.Value); v != "" {
			*l = []string{v}
		}
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*l = items
	case yaml.MappingNode:
		keys, err := mappingKeys(value)
		if err != nil {
			return err
		}
		*l = keys
	}
	return nil
}

// EnvList models `environment` keeping only variable *names* — values never
// enter the profile or the plan (issue #126).
type EnvList []string

// UnmarshalYAML implements yaml.Unmarshaler for both environment forms:
// a list of "KEY=value"/"KEY" entries and a mapping of KEY: value.
func (l *EnvList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		for _, it := range items {
			name, _, _ := strings.Cut(it, "=")
			if name = strings.TrimSpace(name); name != "" {
				*l = append(*l, name)
			}
		}
	case yaml.MappingNode:
		keys, err := mappingKeys(value)
		if err != nil {
			return err
		}
		*l = keys
	}
	return nil
}

// Port is one published/forwarded port. The short syntax
// ("8080:3000", "127.0.0.1:80:8080/tcp", "3000") and the long mapping syntax
// are both supported.
type Port struct {
	HostIP    string
	Published string // host port or range; empty when only a container port is given
	Target    string // container port
	Protocol  string
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *Port) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		p.parseShort(value.Value)
	case yaml.MappingNode:
		var m struct {
			Target    any    `yaml:"target"`
			Published any    `yaml:"published"`
			HostIP    string `yaml:"host_ip"`
			Protocol  string `yaml:"protocol"`
		}
		if err := value.Decode(&m); err != nil {
			return err
		}
		p.Target = scalarString(m.Target)
		p.Published = scalarString(m.Published)
		p.HostIP = m.HostIP
		p.Protocol = m.Protocol
	}
	return nil
}

// parseShort parses the Compose short port syntax.
func (p *Port) parseShort(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	p.Protocol = "tcp"
	if i := strings.LastIndex(s, "/"); i >= 0 {
		p.Protocol = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		p.Target = parts[0]
	case 2:
		p.Published, p.Target = parts[0], parts[1]
	default:
		p.HostIP = strings.Join(parts[:len(parts)-2], ":")
		p.Published, p.Target = parts[len(parts)-2], parts[len(parts)-1]
	}
}

// PublishesAllInterfaces reports whether the service publishes on every host
// interface (the default 0.0.0.0 bind). A service that names a host IP such as
// 127.0.0.1 does not.
func (p Port) PublishesAllInterfaces() bool {
	if p.Target == "" {
		return false
	}
	return p.HostIP == "" || p.HostIP == "0.0.0.0"
}

// HostPort returns the numeric host port, or 0 when none is fixed (a bare
// container port gets an ephemeral host port).
func (p Port) HostPort() int {
	pub := strings.TrimSpace(p.Published)
	if pub == "" {
		return 0
	}
	if i := strings.IndexByte(pub, '-'); i >= 0 {
		pub = pub[:i]
	}
	n, err := strconv.Atoi(pub)
	if err != nil {
		return 0
	}
	return n
}

// Parse parses Compose content. It returns {@link ErrParse} (wrapped) when the
// document is not valid YAML or not a mapping.
func Parse(content []byte) (Document, error) {
	var doc Document
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrParse, err)
	}
	for name, svc := range doc.Services {
		svc.Name = name
		doc.Services[name] = svc
	}
	return doc, nil
}

// ServiceNames returns the service names in deterministic (sorted) order.
func (d Document) ServiceNames() []string {
	names := make([]string, 0, len(d.Services))
	for n := range d.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// mappingKeys returns the keys of a YAML mapping node, sorted.
func mappingKeys(node *yaml.Node) ([]string, error) {
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	sort.Strings(keys)
	return keys, nil
}

// scalarString renders a YAML scalar (string or number) as a string.
func scalarString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
