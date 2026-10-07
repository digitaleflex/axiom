package compose

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServiceBuild is one image Axiom must build for a Compose service. Compose
// build semantics are `docker build` per service context.
type ServiceBuild struct {
	Service    string `json:"service"`
	Context    string `json:"context"`    // relative to the repository root ("." for the root)
	Dockerfile string `json:"dockerfile"` // relative to Context
	Image      string `json:"image"`      // tag Axiom builds and rewrites into the compose file
}

// BuildPlan is the plan-level representation of a Compose build: the ordered
// per-service build inputs, the public service and a rewritten Compose
// document that references the Axiom-built images instead of `build`.
type BuildPlan struct {
	Services []ServiceBuild `json:"services"`
	Public   string         `json:"public,omitempty"`
	// Compose is the rewritten document with each selected buildable service's
	// `build` replaced by its Axiom `image`. It is nil until {@link Rewrite}
	// is called.
	Compose []byte `json:"-"`
}

var nonNameRe = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeComponent lowercases a value and reduces it to the Docker
// repository-name charset.
func sanitizeComponent(s string) string {
	s = nonNameRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "app"
	}
	return s
}

// ImageRef returns the Axiom image reference for a Compose service:
// axiom-<app>-<service>:<commit>.
func ImageRef(app, service, commit string) string {
	return "axiom-" + sanitizeComponent(app) + "-" + sanitizeComponent(service) + ":" + sanitizeComponent(commit)
}

// BuildPlan computes the ordered build inputs for the selected services. The
// order follows {@link ValidationResult.DependencyOrder} so dependencies are
// built first. Compose build contexts default to "." and Dockerfiles to
// "Dockerfile", matching Compose defaults.
func (d Document) BuildPlan(sel ValidationResult, app, commit string) BuildPlan {
	bp := BuildPlan{Public: sel.Public}
	order := sel.DependencyOrder
	if len(order) == 0 {
		order = sel.Services
	}
	for _, name := range order {
		svc, ok := d.Services[name]
		if !ok || svc.Build == nil {
			continue
		}
		context := strings.TrimSpace(svc.Build.Context)
		if context == "" {
			context = "."
		}
		dockerfile := strings.TrimSpace(svc.Build.Dockerfile)
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		bp.Services = append(bp.Services, ServiceBuild{
			Service:    name,
			Context:    context,
			Dockerfile: dockerfile,
			Image:      ImageRef(app, name, commit),
		})
	}
	return bp
}

// Rewrite returns the Compose document with each buildable service's `build`
// replaced by its Axiom `image`, keeping only the given services (nil keeps
// all). The original YAML node tree is mutated so user configuration Axiom
// does not model is preserved verbatim.
func Rewrite(content []byte, bp BuildPlan, keep []string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return content, nil
	}
	services := mappingValue(root.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return content, nil
	}

	images := make(map[string]string, len(bp.Services))
	for _, b := range bp.Services {
		images[b.Service] = b.Image
	}
	keepSet := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepSet[k] = true
	}

	kept := make([]*yaml.Node, 0, len(services.Content))
	for i := 0; i+1 < len(services.Content); i += 2 {
		key, value := services.Content[i], services.Content[i+1]
		if len(keepSet) > 0 && !keepSet[key.Value] {
			continue
		}
		if img, ok := images[key.Value]; ok {
			setMapping(value, "image", img)
			removeMapping(value, "build")
		}
		kept = append(kept, key, value)
	}
	services.Content = kept
	return yaml.Marshal(&root)
}

// mappingValue returns the value node of a mapping key, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMapping sets key to a scalar string, replacing an existing key in place.
func setMapping(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	if existing := mappingValue(node, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		existing.Content = nil
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// removeMapping removes a mapping key if present.
func removeMapping(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}
