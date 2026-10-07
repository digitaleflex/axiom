package docker

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Severity classifies how an issue affects deployability.
//
//   - Error: the Dockerfile cannot be deployed as-is (blocking).
//   - Warn: deployable, but the user should review the construct.
//   - Info: informational only; Axiom compensates at the profile layer.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Stable rule codes (issue #122). Clients branch on these, never on messages.
const (
	// RuleNoFrom: no FROM instruction, or FROM is not the first instruction
	// (ARG is allowed before FROM).
	RuleNoFrom = "DOCKER_NO_FROM"
	// RuleRemoteAdd: ADD fetches a remote http(s) URL. Remote sources must
	// use COPY (or be fetched inside a RUN) so builds stay reproducible.
	RuleRemoteAdd = "DOCKER_REMOTE_ADD"
	// RuleSensitiveVolume: VOLUME mounts a host-absolute sensitive path.
	// Warn-only: the user may have reasons, but it is surfaced for review.
	RuleSensitiveVolume = "DOCKER_VOLUME_SENSITIVE"
	// RuleNoExpose: no EXPOSE instruction. Axiom can still discover the port
	// from the profile/manifest, so this is a warning, not a blocker.
	RuleNoExpose = "DOCKER_NO_EXPOSE"
	// RuleNoCmd: neither CMD nor ENTRYPOINT is present. Informational: the
	// profile layer supplies the runtime command.
	RuleNoCmd = "DOCKER_NO_CMD"
	// RuleNoHealthcheck: no HEALTHCHECK instruction. Informational: Axiom
	// probes health externally instead of baking a probe into the image.
	RuleNoHealthcheck = "DOCKER_NO_HEALTHCHECK"
)

// Issue is one validation finding. Line is the 1-based physical line of the
// instruction that produced it.
type Issue struct {
	Line     int
	Rule     string
	Message  string
	Severity Severity
}

// sensitiveHostPrefixes are host-absolute path prefixes that must never be
// mounted into a container via VOLUME. Mounting them can leak or corrupt host
// state. This is a warn list, not a block: the rule is surfaced for review.
var sensitiveHostPrefixes = []string{
	"/etc",
	"/var",
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/root",
	"/home",
	"/proc",
	"/sys",
	"/dev",
	"/boot",
}

// logicalLine is one Dockerfile instruction after joining continuations.
type logicalLine struct {
	number int    // 1-based physical line where the instruction starts
	text   string // joined instruction text (comment/blank lines removed)
}

// parseLines splits the Dockerfile into logical instruction lines:
// comments (first non-whitespace char '#') and blank lines are dropped, and
// backslash continuations are joined. A '#' inside a continuation is part of
// the command, not a comment.
func parseLines(content []byte) []logicalLine {
	rawLines := strings.Split(string(content), "\n")
	var out []logicalLine
	var buf strings.Builder
	startLine := 0

	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" || strings.HasPrefix(text, "#") {
			return
		}
		out = append(out, logicalLine{number: startLine, text: text})
	}

	for i, raw := range rawLines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if buf.Len() == 0 {
			// Only a fresh (uncontinued) line can be blank or a comment.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			startLine = i + 1
			buf.WriteString(trimmed)
		} else {
			buf.WriteByte(' ')
			buf.WriteString(trimmed)
		}

		if strings.HasSuffix(trimmed, `\`) {
			// Continuation: drop the backslash and keep reading.
			joined := buf.String()
			buf.Reset()
			buf.WriteString(strings.TrimSuffix(joined, `\`) + " ")
			continue
		}
		flush()
	}
	flush()
	return out
}

// instruction is a parsed Dockerfile instruction.
type instruction struct {
	number int
	name   string // uppercased instruction name
	args   string
}

// parseInstructions converts logical lines into instructions.
func parseInstructions(lines []logicalLine) []instruction {
	instructions := make([]instruction, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line.text)
		if len(fields) == 0 {
			continue
		}
		instructions = append(instructions, instruction{
			number: line.number,
			name:   strings.ToUpper(fields[0]),
			args:   strings.TrimSpace(strings.TrimPrefix(line.text, fields[0])),
		})
	}
	return instructions
}

// parseArgs splits instruction arguments, understanding the JSON array form
// (e.g. `ADD ["http://x", "/dest"]`). Falls back to whitespace splitting.
func parseArgs(args string) []string {
	trimmed := strings.TrimSpace(args)
	if strings.HasPrefix(trimmed, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return arr
		}
	}
	return strings.Fields(trimmed)
}

// addSources returns the source operands of an ADD instruction (everything
// except the destination, flags excluded).
func addSources(inst instruction) []string {
	var positional []string
	for _, arg := range parseArgs(inst.args) {
		if strings.HasPrefix(arg, "--") {
			continue // --from, --chown, --checksum, ...
		}
		positional = append(positional, arg)
	}
	if len(positional) < 2 {
		return nil
	}
	return positional[:len(positional)-1]
}

// isRemoteURL reports whether an ADD source is a remote http(s) URL.
func isRemoteURL(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

// volumePaths returns the mount paths of a VOLUME instruction.
func volumePaths(inst instruction) []string {
	return parseArgs(inst.args)
}

// isSensitiveHostPath reports whether a VOLUME path is host-absolute and
// under a sensitive prefix. Named volumes (no leading '/') never match.
func isSensitiveHostPath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for _, prefix := range sensitiveHostPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// exposePorts returns the numeric ports of an EXPOSE instruction, in order.
// Protocol suffixes (/tcp, /udp) are stripped; variables are skipped.
func exposePorts(inst instruction) []int {
	var ports []int
	for _, arg := range parseArgs(inst.args) {
		if i := strings.Index(arg, "/"); i >= 0 {
			arg = arg[:i]
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		ports = append(ports, n)
	}
	return ports
}

// ValidateDockerfile validates repository Dockerfile content for Axiom
// (issue #122) and returns the issues found, each with a stable rule code and
// severity. An empty slice means the Dockerfile is deployable as-is.
//
// Rules:
//   - FROM required as the first instruction (ARG allowed before) — error.
//   - ADD of remote http(s) URLs forbidden (COPY only for remote) — error.
//   - VOLUME of host-absolute sensitive paths — warning (review list).
//   - EXPOSE missing — warning (port can still come from the profile).
//   - CMD/ENTRYPOINT missing — info (profile supplies the command).
//   - HEALTHCHECK missing — info (Axiom probes health externally).
func ValidateDockerfile(content []byte) []Issue {
	instructions := parseInstructions(parseLines(content))
	var issues []Issue

	sawFrom := false
	fromFirst := false
	exposeCount := 0
	cmdCount := 0
	entrypointCount := 0
	healthcheckCount := 0

	for i, inst := range instructions {
		switch inst.name {
		case "FROM":
			sawFrom = true
			// FROM must be the first instruction; only ARG may precede it.
			if i == 0 || (i == 1 && instructions[0].name == "ARG") {
				fromFirst = true
			}
		case "ADD":
			for _, source := range addSources(inst) {
				if isRemoteURL(source) {
					issues = append(issues, Issue{
						Line:     inst.number,
						Rule:     RuleRemoteAdd,
						Severity: SeverityError,
						Message:  "ADD fetches a remote URL (" + source + "); use COPY for remote sources so builds stay reproducible",
					})
				}
			}
		case "VOLUME":
			for _, path := range volumePaths(inst) {
				if isSensitiveHostPath(path) {
					issues = append(issues, Issue{
						Line:     inst.number,
						Rule:     RuleSensitiveVolume,
						Severity: SeverityWarn,
						Message:  "VOLUME mounts host path " + path + ", which can leak or corrupt host state",
					})
				}
			}
		case "EXPOSE":
			exposeCount++
		case "CMD":
			cmdCount++
		case "ENTRYPOINT":
			entrypointCount++
		case "HEALTHCHECK":
			healthcheckCount++
		}
	}

	if !sawFrom {
		issues = append(issues, Issue{
			Line:     0,
			Rule:     RuleNoFrom,
			Severity: SeverityError,
			Message:  "no FROM instruction: a Dockerfile must start from a base image",
		})
	} else if !fromFirst {
		issues = append(issues, Issue{
			Line:     0,
			Rule:     RuleNoFrom,
			Severity: SeverityError,
			Message:  "FROM must be the first instruction (only ARG may precede it)",
		})
	}

	if exposeCount == 0 {
		issues = append(issues, Issue{
			Line:     0,
			Rule:     RuleNoExpose,
			Severity: SeverityWarn,
			Message:  "no EXPOSE instruction: Axiom will use the port from the application profile",
		})
	}

	if cmdCount == 0 && entrypointCount == 0 {
		issues = append(issues, Issue{
			Line:     0,
			Rule:     RuleNoCmd,
			Severity: SeverityInfo,
			Message:  "no CMD or ENTRYPOINT: the runtime command comes from the application profile",
		})
	}

	if healthcheckCount == 0 {
		issues = append(issues, Issue{
			Line:     0,
			Rule:     RuleNoHealthcheck,
			Severity: SeverityInfo,
			Message:  "no HEALTHCHECK: Axiom probes health externally instead of baking a probe into the image",
		})
	}

	return issues
}

// ValidateSnapshot loads the Dockerfile from the snapshot and validates it.
// The second return value is false when no Dockerfile exists at the path.
func ValidateSnapshot(files FileAccessor, path string) ([]Issue, bool) {
	content, ok := LoadDockerfile(files, path)
	if !ok {
		return nil, false
	}
	return ValidateDockerfile(content), true
}
