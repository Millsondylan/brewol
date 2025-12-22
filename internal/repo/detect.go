// Package repo provides project detection and verification runner functionality.
package repo

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectType represents the detected project type
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeRust    ProjectType = "rust"
	ProjectTypeJava    ProjectType = "java"
	ProjectTypeMake    ProjectType = "make"
	ProjectTypeUnknown ProjectType = "unknown"
)

// Project represents a detected project
type Project struct {
	Type           ProjectType
	Root           string
	Name           string
	TestCommand    string
	BuildCommand   string
	LintCommand    string
	FormatCommand  string
	PackageManager string
}

// DetectProject detects the project type and configuration
func DetectProject(root string) *Project {
	p := &Project{
		Root: root,
		Type: ProjectTypeUnknown,
	}

	// Check for Go project
	if fileExists(filepath.Join(root, "go.mod")) {
		p.Type = ProjectTypeGo
		p.Name = getGoModuleName(root)
		p.TestCommand = "go test ./..."
		p.BuildCommand = "go build ./..."
		p.LintCommand = "golangci-lint run"
		p.FormatCommand = "gofmt -w ."
		return p
	}

	// Check for Node.js project
	if fileExists(filepath.Join(root, "package.json")) {
		p.Type = ProjectTypeNode
		p.Name = getPackageJsonName(root)

		// Determine package manager
		if fileExists(filepath.Join(root, "pnpm-lock.yaml")) {
			p.PackageManager = "pnpm"
			p.TestCommand = "pnpm test"
			p.BuildCommand = "pnpm build"
			p.LintCommand = "pnpm lint"
			p.FormatCommand = "pnpm format"
		} else if fileExists(filepath.Join(root, "yarn.lock")) {
			p.PackageManager = "yarn"
			p.TestCommand = "yarn test"
			p.BuildCommand = "yarn build"
			p.LintCommand = "yarn lint"
			p.FormatCommand = "yarn format"
		} else {
			p.PackageManager = "npm"
			p.TestCommand = "npm test"
			p.BuildCommand = "npm run build"
			p.LintCommand = "npm run lint"
			p.FormatCommand = "npm run format"
		}
		return p
	}

	// Check for Python project
	if fileExists(filepath.Join(root, "pyproject.toml")) ||
		fileExists(filepath.Join(root, "setup.py")) ||
		fileExists(filepath.Join(root, "requirements.txt")) {
		p.Type = ProjectTypePython
		p.TestCommand = "pytest"
		p.LintCommand = "ruff check ."
		p.FormatCommand = "ruff format ."

		if fileExists(filepath.Join(root, "pyproject.toml")) {
			p.Name = getPyprojectName(root)
		}
		return p
	}

	// Check for Rust project
	if fileExists(filepath.Join(root, "Cargo.toml")) {
		p.Type = ProjectTypeRust
		p.Name = getCargoName(root)
		p.TestCommand = "cargo test"
		p.BuildCommand = "cargo build"
		p.LintCommand = "cargo clippy"
		p.FormatCommand = "cargo fmt"
		return p
	}

	// Check for Java project
	if fileExists(filepath.Join(root, "pom.xml")) {
		p.Type = ProjectTypeJava
		p.TestCommand = "mvn test"
		p.BuildCommand = "mvn package"
		return p
	}

	if fileExists(filepath.Join(root, "build.gradle")) || fileExists(filepath.Join(root, "build.gradle.kts")) {
		p.Type = ProjectTypeJava
		p.TestCommand = "./gradlew test"
		p.BuildCommand = "./gradlew build"
		return p
	}

	// Check for Makefile
	if fileExists(filepath.Join(root, "Makefile")) {
		p.Type = ProjectTypeMake
		if makefileHasTarget(root, "test") {
			p.TestCommand = "make test"
		}
		if makefileHasTarget(root, "build") {
			p.BuildCommand = "make build"
		}
		if makefileHasTarget(root, "lint") {
			p.LintCommand = "make lint"
		}
		if makefileHasTarget(root, "format") {
			p.FormatCommand = "make format"
		}
		return p
	}

	return p
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getGoModuleName extracts the module name from go.mod
func getGoModuleName(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// getPackageJsonName extracts the name from package.json
func getPackageJsonName(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return ""
	}

	// Simple extraction without full JSON parsing
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"name"`) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `",`)
				return name
			}
		}
	}
	return ""
}

// getPyprojectName extracts the name from pyproject.toml
func getPyprojectName(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				return name
			}
		}
	}
	return ""
}

// getCargoName extracts the name from Cargo.toml
func getCargoName(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				return name
			}
		}
	}
	return ""
}

// makefileHasTarget checks if a Makefile has a specific target
func makefileHasTarget(root, target string) bool {
	content, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}
