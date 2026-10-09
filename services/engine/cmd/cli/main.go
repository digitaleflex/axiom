package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: cli <status|build|containers>")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		statusCmd(os.Args[2:])
	case "build":
		buildCmd(os.Args[2:])
	case "containers":
		containersCmd(os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func statusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Parse(args)

	// module Go
	modPath := filepath.Join("..", "..", "go.mod")
	modData, err := os.ReadFile(modPath)
	if err == nil {
		for _, line := range strings.Split(string(modData), "\n") {
			if strings.HasPrefix(line, "module ") {
				fmt.Printf("Module: %s\n", strings.TrimSpace(strings.TrimPrefix(line, "module")))
				break
			}
		}
	}

	// version (git describe ou hardcodé)
	out, err := exec.Command("git", "describe", "--tags", "--always").Output()
	ver := string(out)
	if err != nil || ver == "" {
		ver = "unknown"
	}
	fmt.Printf("Version: %s\n", strings.TrimSpace(ver))
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	fs.Parse(args)

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Println("Building services/engine/ ...")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Build failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Build OK")
}

func containersCmd(args []string) {
	fs := flag.NewFlagSet("containers", flag.ExitOnError)
	fs.Parse(args)

	composePath := filepath.Join("..", "..", "docker-compose.dev.yml")
	if _, err := os.Stat(composePath); err != nil {
		fmt.Println("docker-compose.dev.yml not found — containers unavailable")
		return
	}

	cmd := exec.Command("docker", "compose", "-f", composePath, "ps")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Containers status unavailable: %v\n", err)
	} else {
		fmt.Println("Containers status shown above")
	}
}
