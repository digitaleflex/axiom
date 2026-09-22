package profile

import (
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer"
)

type ApplicationProfile struct {
	Version          int
	Ref              string
	Language         string
	Framework        string
	PackageManager   string
	BuildCommand     string
	StartCommand     string
	Port             int
	ContainerStrategy string
	Services         []string
	Configuration    []string
	Confidence       float64
	Evidence         []string
}

type Builder struct{}

func NewBuilder() *Builder { return &Builder{} }

func (b *Builder) Build(result analyzer.Result) (ApplicationProfile, error) {
	p := ApplicationProfile{Version: 1, Ref: result.Ref}
	var confidence float64
	var count int
	for _, f := range result.Findings {
		switch f.Kind {
		case "language":
			if p.Language == "" { p.Language = f.Value }
		case "framework":
			if p.Framework == "" { p.Framework = f.Value }
		case "package_manager":
			if p.PackageManager == "" { p.PackageManager = f.Value }
		case "container":
			if p.ContainerStrategy == "" { p.ContainerStrategy = f.Value }
		}
		p.Evidence = append(p.Evidence, f.Evidence...)
		if f.Confidence > 0 { confidence += f.Confidence; count++ }
	}
	switch p.PackageManager {
	case "pnpm": p.BuildCommand, p.StartCommand = "pnpm build", "pnpm start"
	case "yarn": p.BuildCommand, p.StartCommand = "yarn build", "yarn start"
	case "npm": p.BuildCommand, p.StartCommand = "npm run build", "npm start"
	case "bun": p.BuildCommand, p.StartCommand = "bun run build", "bun run start"
	case "go": p.BuildCommand, p.StartCommand = "go build ./...", "./app"
	default:
		if p.Language == "Go" { p.BuildCommand, p.StartCommand = "go build ./...", "./app" }
	}
	switch p.Framework {
	case "Next.js", "Vite":
		p.Port = 3000
	default:
		if p.Language == "Go" { p.Port = 8080 }
	}
	if p.ContainerStrategy == "" { p.ContainerStrategy = "docker" }
	if count > 0 { p.Confidence = confidence / float64(count) }
	if p.Language == "" || p.BuildCommand == "" || p.StartCommand == "" || p.Port == 0 {
		return ApplicationProfile{}, fmt.Errorf("insufficient evidence to build a deployable application profile")
	}
	return p, nil
}
