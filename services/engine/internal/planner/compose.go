package planner

import (
	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
	"github.com/digitaleflex/axiom/services/engine/internal/profile/presets"
)

// validateComposeSelection enforces the Compose service-selection contract on a
// generated plan (issue #123):
//
//   - a compose plan must select at least one service;
//   - service names must be unique and non-empty;
//   - the public service, when set, must be one of the selected services;
//   - a dependency order, when present, must be exactly the selected services
//     (a topological order is produced by the compose preset, which owns the
//     dependency graph; the planner only checks the order is a permutation).
//
// Errors are field-addressable like the rest of plan validation so the UI can
// point at the field to fix.
func validateComposeSelection(p Plan) validation.Errors {
	if p.Build.Strategy != presets.StrategyCompose {
		return nil
	}
	var errs validation.Errors
	add := func(field, msg string) { errs = append(errs, validation.FieldError{Field: field, Message: msg}) }

	if len(p.Runtime.Services) == 0 {
		add("runtime.services", "a compose plan must select at least one service")
	}
	selected := make(map[string]bool, len(p.Runtime.Services))
	for _, s := range p.Runtime.Services {
		if s == "" {
			add("runtime.services", "service names must not be empty")
			continue
		}
		if selected[s] {
			add("runtime.services", "duplicate service "+s)
		}
		selected[s] = true
	}
	if pub := p.Network.PublicService; pub != "" && !selected[pub] {
		add("network.publicService", "public service "+pub+" is not one of the selected services")
	}
	if order := p.Runtime.DependencyOrder; len(order) > 0 && !sameServiceSet(order, p.Runtime.Services) {
		add("runtime.dependencyOrder", "dependency order must contain exactly the selected services")
	}
	return errs
}

// sameServiceSet reports whether a and b contain the same services with the
// same multiplicities.
func sameServiceSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}
