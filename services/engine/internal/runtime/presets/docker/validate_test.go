package docker

import (
	"testing"
)

func rules(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Rule)
	}
	return out
}

func hasRule(issues []Issue, rule string) bool {
	for _, issue := range issues {
		if issue.Rule == rule {
			return true
		}
	}
	return false
}

func severityOf(issues []Issue, rule string) (Severity, bool) {
	for _, issue := range issues {
		if issue.Rule == rule {
			return issue.Severity, true
		}
	}
	return "", false
}

// A clean, minimal Dockerfile produces no issues.
func TestValidateCleanDockerfile(t *testing.T) {
	issues := ValidateDockerfile([]byte("FROM node:20-alpine\nWORKDIR /app\nCOPY . .\nRUN npm ci\nEXPOSE 3000\nHEALTHCHECK CMD curl -f http://localhost:3000/ || exit 1\nCMD [\"node\", \"server.js\"]\n"))
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

// Multi-stage Dockerfiles are valid: several FROMs, ARG before FROM,
// comments and blank lines anywhere.
func TestValidateMultiStageDockerfile(t *testing.T) {
	content := `# syntax=docker/dockerfile:1
ARG NODE_VERSION=20-alpine

FROM node:${NODE_VERSION} AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

# Build stage
FROM node:${NODE_VERSION} AS build
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:${NODE_VERSION}
WORKDIR /app
COPY --from=build /app/dist ./dist
EXPOSE 3000
HEALTHCHECK CMD curl -f http://localhost:3000/ || exit 1
CMD ["node", "dist/server.js"]
`
	issues := ValidateDockerfile([]byte(content))
	if len(issues) != 0 {
		t.Fatalf("multi-stage Dockerfile should be clean, got %+v", issues)
	}
}

// ARG before FROM is allowed; any other instruction before FROM is not.
func TestValidateArgBeforeFrom(t *testing.T) {
	issues := ValidateDockerfile([]byte("ARG VERSION=1\nFROM node:${VERSION}-alpine\nEXPOSE 3000\n"))
	if hasRule(issues, RuleNoFrom) {
		t.Fatalf("ARG before FROM must be allowed, got %+v", issues)
	}

	issues = ValidateDockerfile([]byte("RUN echo hi\nFROM node:20-alpine\n"))
	if !hasRule(issues, RuleNoFrom) {
		t.Fatalf("instruction before FROM must be rejected, got %+v", issues)
	}
	if sev, _ := severityOf(issues, RuleNoFrom); sev != SeverityError {
		t.Fatalf("DOCKER_NO_FROM severity = %q, want error", sev)
	}
}

// A Dockerfile without any FROM is rejected.
func TestValidateNoFrom(t *testing.T) {
	issues := ValidateDockerfile([]byte("WORKDIR /app\nCOPY . .\n"))
	if !hasRule(issues, RuleNoFrom) {
		t.Fatalf("missing FROM must be rejected, got %+v", issues)
	}
}

// Remote ADD is rejected in both shell and JSON array form.
func TestValidateRemoteAdd(t *testing.T) {
	cases := map[string]string{
		"shell form":  "FROM node:20-alpine\nADD https://example.com/app.tgz /app/\n",
		"json form":   "FROM node:20-alpine\nADD [\"https://example.com/app.tgz\", \"/app/\"]\n",
		"http scheme": "FROM node:20-alpine\nADD http://example.com/x /x\n",
		"with flag":   "FROM node:20-alpine\nADD --chown=1000 https://example.com/app.tgz /app/\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			issues := ValidateDockerfile([]byte(content))
			if !hasRule(issues, RuleRemoteAdd) {
				t.Fatalf("remote ADD must be rejected, got %+v", issues)
			}
			if sev, _ := severityOf(issues, RuleRemoteAdd); sev != SeverityError {
				t.Fatalf("DOCKER_REMOTE_ADD severity = %q, want error", sev)
			}
		})
	}
}

// COPY of a remote URL is allowed (COPY only for remote is the rule for ADD).
func TestValidateRemoteCopyAllowed(t *testing.T) {
	issues := ValidateDockerfile([]byte("FROM node:20-alpine\nCOPY https://example.com/x /x\nEXPOSE 80\n"))
	if hasRule(issues, RuleRemoteAdd) {
		t.Fatalf("remote COPY must be allowed, got %+v", issues)
	}
}

// VOLUME of host-absolute sensitive paths is a warning, not a block.
func TestValidateSensitiveVolume(t *testing.T) {
	issues := ValidateDockerfile([]byte("FROM node:20-alpine\nVOLUME /etc /data\nEXPOSE 3000\n"))
	if !hasRule(issues, RuleSensitiveVolume) {
		t.Fatalf("sensitive VOLUME must be flagged, got %+v", issues)
	}
	if sev, _ := severityOf(issues, RuleSensitiveVolume); sev != SeverityWarn {
		t.Fatalf("DOCKER_VOLUME_SENSITIVE severity = %q, want warn", sev)
	}

	// JSON array form and non-sensitive paths.
	issues = ValidateDockerfile([]byte("FROM node:20-alpine\nVOLUME [\"/data\", \"/logs\"]\nEXPOSE 3000\n"))
	if hasRule(issues, RuleSensitiveVolume) {
		t.Fatalf("non-sensitive VOLUME must not be flagged, got %+v", issues)
	}

	// Named volumes are not host paths.
	issues = ValidateDockerfile([]byte("FROM node:20-alpine\nVOLUME data\nEXPOSE 3000\n"))
	if hasRule(issues, RuleSensitiveVolume) {
		t.Fatalf("named volume must not be flagged, got %+v", issues)
	}
}

// Missing EXPOSE is a warning.
func TestValidateNoExpose(t *testing.T) {
	issues := ValidateDockerfile([]byte("FROM node:20-alpine\nCMD [\"node\"]\n"))
	if !hasRule(issues, RuleNoExpose) {
		t.Fatalf("missing EXPOSE must be flagged, got %+v", issues)
	}
	if sev, _ := severityOf(issues, RuleNoExpose); sev != SeverityWarn {
		t.Fatalf("DOCKER_NO_EXPOSE severity = %q, want warn", sev)
	}
}

// Missing CMD/ENTRYPOINT and HEALTHCHECK are informational.
func TestValidateInformationalRules(t *testing.T) {
	issues := ValidateDockerfile([]byte("FROM node:20-alpine\nEXPOSE 3000\n"))
	for _, rule := range []string{RuleNoCmd, RuleNoHealthcheck} {
		if !hasRule(issues, rule) {
			t.Fatalf("%s must be flagged, got %+v", rule, issues)
		}
		if sev, _ := severityOf(issues, rule); sev != SeverityInfo {
			t.Fatalf("%s severity = %q, want info", rule, sev)
		}
	}

	// ENTRYPOINT alone satisfies the command rule.
	issues = ValidateDockerfile([]byte("FROM node:20-alpine\nEXPOSE 3000\nENTRYPOINT [\"node\"]\n"))
	if hasRule(issues, RuleNoCmd) {
		t.Fatalf("ENTRYPOINT satisfies the command rule, got %+v", issues)
	}

	// HEALTHCHECK NONE still counts as present.
	issues = ValidateDockerfile([]byte("FROM node:20-alpine\nEXPOSE 3000\nHEALTHCHECK NONE\n"))
	if hasRule(issues, RuleNoHealthcheck) {
		t.Fatalf("HEALTHCHECK NONE counts as present, got %+v", issues)
	}
}

// Line continuations are joined before parsing.
func TestValidateLineContinuations(t *testing.T) {
	content := "FROM node:20-alpine\nEXPOSE \\\n  3000 \\\n  8080\nCMD [\"node\"]\n"
	issues := ValidateDockerfile([]byte(content))
	if hasRule(issues, RuleNoExpose) {
		t.Fatalf("continued EXPOSE must be parsed, got %+v", issues)
	}
}

// Issue line numbers point at the physical instruction line.
func TestValidateIssueLineNumbers(t *testing.T) {
	content := "# comment\n\nFROM node:20-alpine\n\nADD https://example.com/x /x\n"
	issues := ValidateDockerfile([]byte(content))
	found := false
	for _, issue := range issues {
		if issue.Rule == RuleRemoteAdd {
			found = true
			if issue.Line != 5 {
				t.Fatalf("issue line = %d, want 5", issue.Line)
			}
		}
	}
	if !found {
		t.Fatalf("remote ADD issue not found in %+v", issues)
	}
}

// ValidateSnapshot loads and validates in one call.
func TestValidateSnapshot(t *testing.T) {
	files := snapshot(SnapshotFile{Path: "Dockerfile", Content: []byte("FROM node:20-alpine\nEXPOSE 3000\nHEALTHCHECK CMD curl -f http://localhost:3000/ || exit 1\nCMD [\"node\"]\n")})
	issues, ok := ValidateSnapshot(files, "")
	if !ok {
		t.Fatal("snapshot should contain a Dockerfile")
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}

	if _, ok := ValidateSnapshot(snapshot(), ""); ok {
		t.Fatal("empty snapshot must report false")
	}
}
