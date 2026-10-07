package compose

import (
	"sort"
	"strings"
)

// ValidationResult is the outcome of service selection: the services Axiom
// will deploy, the public service, their deterministic dependency order, and
// every validation issue found (structural and selection-specific).
type ValidationResult struct {
	// Services is the selected service set in deterministic (sorted) order.
	Services []string
	// Public is the service that receives public traffic. Empty when no
	// selected service publishes a port.
	Public string
	// DependencyOrder is a topological order of the selected services
	// (dependencies first). It is partial when a cycle was detected.
	DependencyOrder []string
	// Issues are all findings, structural and selection-specific.
	Issues []Issue
}

// HasErrors reports whether any finding is blocking.
func (r ValidationResult) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only the blocking findings.
func (r ValidationResult) Errors() []Issue {
	var out []Issue
	for _, i := range r.Issues {
		if i.Severity == SeverityError {
			out = append(out, i)
		}
	}
	return out
}

// SelectServices validates the document, resolves the selected services and
// their dependency order, and determines the public service.
//
// include is the manifest `services.include` list; an empty include selects
// every service. Selection is closed under depends_on: a service cannot start
// without its dependencies, so dependencies of selected services are added
// transitively (an unknown dependency is an error).
//
// public is the manifest `services.public` value. When empty, the first
// selected service (in dependency order) that publishes a port is chosen; when
// no selected service publishes a port, RuleNoPublicService is warned.
func (d Document) SelectServices(include []string, public string) ValidationResult {
	res := ValidationResult{Issues: d.Validate()}
	if len(d.Services) == 0 {
		return res
	}

	selected := map[string]bool{}
	if len(include) == 0 {
		for _, n := range d.ServiceNames() {
			selected[n] = true
		}
	} else {
		for _, n := range include {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, ok := d.Services[n]; !ok {
				res.Issues = append(res.Issues, issue("", RuleUnknownService, SeverityError,
					"included service "+n+" does not exist in the compose file"))
				continue
			}
			selected[n] = true
		}
	}

	// Close the selection under depends_on (breadth-first, deterministic).
	unknown := map[string]bool{}
	queue := sortedKeys(selected)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, dep := range d.Services[name].DependsOn {
			if _, ok := d.Services[dep]; !ok {
				unknown[dep] = true
				continue
			}
			if !selected[dep] {
				selected[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	for _, dep := range sortedKeys(unknown) {
		res.Issues = append(res.Issues, issue("", RuleUnknownDependency, SeverityError,
			"a selected service depends on unknown service "+dep))
	}

	services := sortedKeys(selected)
	order, cycle := topoSort(d.Services, selected)
	if cycle {
		res.Issues = append(res.Issues, issue("", RuleDependencyCycle, SeverityError,
			"the selected services form a depends_on cycle: "+strings.Join(order, " -> ")))
		order = services
	}

	// Resolve the public service.
	if public == "" {
		if cands := publishingServices(order, d.Services); len(cands) > 0 {
			public = cands[0]
		} else {
			res.Issues = append(res.Issues, issue("", RuleNoPublicService, SeverityWarn,
				"no selected service publishes a port; Axiom cannot route traffic to the application"))
		}
	} else if !selected[public] {
		res.Issues = append(res.Issues, issue("", RulePublicNotIncluded, SeverityError,
			"public service "+public+" is not part of the selected services"))
	}

	// A non-public service must not publish on all host interfaces: that would
	// expose it directly, bypassing Axiom's routing boundary.
	for _, name := range services {
		if name == public {
			continue
		}
		for _, p := range d.Services[name].Ports {
			if p.PublishesAllInterfaces() {
				res.Issues = append(res.Issues, issue(name, RulePublicHostBind, SeverityError,
					"non-public service "+name+" publishes on all host interfaces (0.0.0.0)"))
				break
			}
		}
	}

	if !hasBuildable(d.Services, selected) {
		res.Issues = append(res.Issues, issue("", RuleNoBuildableService, SeverityWarn,
			"no selected service defines a build section"))
	}

	res.Services = services
	res.Public = public
	res.DependencyOrder = order
	return res
}

// topoSort returns a deterministic topological order of the selected services
// (dependencies first). The second return value is true when a cycle prevents
// a complete ordering; order then contains the acyclic prefix.
func topoSort(services map[string]Service, selected map[string]bool) ([]string, bool) {
	indeg := make(map[string]int, len(selected))
	dependents := make(map[string][]string, len(selected))
	for n := range selected {
		indeg[n] = 0
	}
	for n := range selected {
		seen := map[string]bool{}
		for _, dep := range services[n].DependsOn {
			if !selected[dep] || seen[dep] {
				continue
			}
			seen[dep] = true
			indeg[n]++
			dependents[dep] = append(dependents[dep], n)
		}
	}
	ready := make([]string, 0, len(selected))
	for n, deg := range indeg {
		if deg == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(selected))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		next := append([]string(nil), dependents[name]...)
		sort.Strings(next)
		for _, m := range next {
			indeg[m]--
			if indeg[m] == 0 {
				ready = append(ready, m)
			}
		}
		sort.Strings(ready)
	}
	return order, len(order) != len(selected)
}

// publishingServices returns the services that publish at least one port, in
// the given order.
func publishingServices(order []string, services map[string]Service) []string {
	var out []string
	for _, name := range order {
		if len(services[name].Ports) > 0 {
			out = append(out, name)
		}
	}
	return out
}

// hasBuildable reports whether any selected service has a build section.
func hasBuildable(services map[string]Service, selected map[string]bool) bool {
	for n := range selected {
		if services[n].Build != nil {
			return true
		}
	}
	return false
}

// sortedKeys returns the keys of a set in sorted order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
