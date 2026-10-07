package compose

import (
	"path"
	"strconv"
	"strings"
)

// Severity classifies how an issue affects deployability.
//
//   - Error: the compose file cannot be deployed as-is (blocking).
//   - Warn: deployable, but the user should review the construct.
//   - Info: informational only.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Stable rule codes (issue #123). Clients branch on these, never on messages.
const (
	// RuleNoServices: the compose file defines no services.
	RuleNoServices = "COMPOSE_NO_SERVICES"
	// RulePrivileged: a service requests privileged: true.
	RulePrivileged = "COMPOSE_PRIVILEGED"
	// RuleNetworkModeHost: a service uses network_mode: host.
	RuleNetworkModeHost = "COMPOSE_NETWORK_MODE_HOST"
	// RulePIDHost: a service uses pid: host.
	RulePIDHost = "COMPOSE_PID_HOST"
	// RuleIPCHost: a service uses ipc: host.
	RuleIPCHost = "COMPOSE_IPC_HOST"
	// RuleHostMountSensitive: a service bind-mounts a sensitive host path.
	RuleHostMountSensitive = "COMPOSE_HOST_MOUNT_SENSITIVE"
	// RuleVolumesFrom: a service uses volumes_from (warning, review only).
	RuleVolumesFrom = "COMPOSE_VOLUMES_FROM"
	// RulePrivilegedPort: a service publishes a host port below 1024.
	RulePrivilegedPort = "COMPOSE_PRIVILEGED_PORT"
	// RuleHostPublishAll: a service publishes on all host interfaces (info).
	RuleHostPublishAll = "COMPOSE_HOST_PUBLISH_ALL"
	// RulePublicHostBind: a non-public service publishes on all host
	// interfaces, which would expose it directly.
	RulePublicHostBind = "COMPOSE_PUBLIC_HOST_BIND"
	// RuleUnknownService: an included service does not exist.
	RuleUnknownService = "COMPOSE_UNKNOWN_SERVICE"
	// RulePublicNotIncluded: the public service is not part of the selection.
	RulePublicNotIncluded = "COMPOSE_PUBLIC_NOT_INCLUDED"
	// RuleUnknownDependency: depends_on references a non-existent service.
	RuleUnknownDependency = "COMPOSE_UNKNOWN_DEPENDENCY"
	// RuleDependencyCycle: the selected dependency graph has a cycle.
	RuleDependencyCycle = "COMPOSE_DEPENDENCY_CYCLE"
	// RuleNoPublicService: no selected service publishes a port.
	RuleNoPublicService = "COMPOSE_NO_PUBLIC_SERVICE"
	// RuleNoBuildableService: no selected service has a build section.
	RuleNoBuildableService = "COMPOSE_NO_BUILDABLE_SERVICE"
)

// Issue is one validation finding. Service names the offending service when
// the rule is service-scoped, and is empty for document-level rules.
type Issue struct {
	Service  string   `json:"service,omitempty"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// sensitiveHostPaths are host-absolute paths that must never be bind-mounted
// into an Axiom-deployed container. Mounting them leaks or corrupts host
// state, or grants control over the container runtime (docker.sock).
var sensitiveHostPaths = []string{
	"/",
	"/etc",
	"/root",
	"/home",
	"/var/run/docker.sock",
}

// isSensitiveHostPath reports whether p is a host-absolute path under a
// sensitive prefix. Named volumes (no leading '/') and relative binds never
// match.
func isSensitiveHostPath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	for _, s := range sensitiveHostPaths {
		if clean == s {
			return true
		}
		if s != "/" && strings.HasPrefix(clean, s+"/") {
			return true
		}
	}
	return false
}

// volumeSource returns the source of a Compose volume entry
// ("source:target[:mode]"). It returns "" for anonymous volumes and for
// entries without a source.
func volumeSource(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	parts := strings.Split(entry, ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

// Validate checks the document against Axiom's Compose security policy and
// returns every finding. An empty slice means the document is deployable.
//
// Rules:
//   - no services — error.
//   - privileged: true — error.
//   - network_mode: host / pid: host / ipc: host — error.
//   - bind mount of a sensitive host path — error.
//   - volumes_from — warning.
//   - host port below 1024 — error.
//   - publish on all host interfaces — info (escalated to an error for
//     non-public services by {@link SelectServices}, which knows the public
//     service).
func (d Document) Validate() []Issue {
	if len(d.Services) == 0 {
		return []Issue{{
			Rule: RuleNoServices, Severity: SeverityError,
			Message: "the compose file defines no services",
		}}
	}
	var issues []Issue
	for _, name := range d.ServiceNames() {
		svc := d.Services[name]
		if svc.Privileged {
			issues = append(issues, issue(name, RulePrivileged, SeverityError,
				"service requests privileged: true, which grants host control"))
		}
		if strings.EqualFold(svc.NetworkMode, "host") {
			issues = append(issues, issue(name, RuleNetworkModeHost, SeverityError,
				"network_mode: host gives the service the host network namespace"))
		}
		if strings.EqualFold(svc.PID, "host") {
			issues = append(issues, issue(name, RulePIDHost, SeverityError,
				"pid: host gives the service the host PID namespace"))
		}
		if strings.EqualFold(svc.IPC, "host") {
			issues = append(issues, issue(name, RuleIPCHost, SeverityError,
				"ipc: host gives the service the host IPC namespace"))
		}
		for _, v := range svc.Volumes {
			if src := volumeSource(v); isSensitiveHostPath(src) {
				issues = append(issues, issue(name, RuleHostMountSensitive, SeverityError,
					"service bind-mounts sensitive host path "+src))
			}
		}
		if len(svc.VolumesFrom) > 0 {
			issues = append(issues, issue(name, RuleVolumesFrom, SeverityWarn,
				"service uses volumes_from, which shares another container's volumes"))
		}
		for _, p := range svc.Ports {
			if hp := p.HostPort(); hp > 0 && hp < 1024 {
				issues = append(issues, issue(name, RulePrivilegedPort, SeverityError,
					"service publishes privileged host port "+strconv.Itoa(hp)))
			}
			if p.PublishesAllInterfaces() {
				issues = append(issues, issue(name, RuleHostPublishAll, SeverityInfo,
					"service publishes on all host interfaces (0.0.0.0)"))
			}
		}
	}
	return issues
}

func issue(service, rule string, severity Severity, message string) Issue {
	return Issue{Service: service, Rule: rule, Severity: severity, Message: message}
}
