package docker

// PortInfo is the port discovery result (issue #122).
//
// Precedence is decided by the profile layer, not here: an explicit manifest
// (axiom.yaml) port override wins over {@link PortInfo.Preferred}, which is
// the first EXPOSE port. This package deliberately knows nothing about
// manifests — it reports the facts from the Dockerfile and lets the profile
// layer combine them with overrides.
type PortInfo struct {
	// Ports are all EXPOSE ports, in declaration order.
	Ports []int
	// Preferred is the first EXPOSE port, or 0 when the Dockerfile exposes
	// none.
	Preferred int
	// HasCMD reports whether the Dockerfile defines a default command
	// (CMD or ENTRYPOINT). The profile layer uses this to decide whether a
	// runtime command must be supplied.
	HasCMD bool
}

// DiscoverPorts extracts port and command facts from Dockerfile content.
//
// Only EXPOSE is consulted for ports; there is no manifest concept in this
// package. The profile layer applies manifest overrides on top of
// {@link PortInfo.Preferred}.
func DiscoverPorts(content []byte) PortInfo {
	instructions := parseInstructions(parseLines(content))
	info := PortInfo{}
	for _, inst := range instructions {
		switch inst.name {
		case "EXPOSE":
			info.Ports = append(info.Ports, exposePorts(inst)...)
		case "CMD", "ENTRYPOINT":
			info.HasCMD = true
		}
	}
	if len(info.Ports) > 0 {
		info.Preferred = info.Ports[0]
	}
	return info
}
