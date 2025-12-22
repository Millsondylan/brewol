package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProject_Go(t *testing.T) {
	tempDir := t.TempDir()

	// Create go.mod
	goMod := `module github.com/test/myproject

go 1.21
`
	os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeGo {
		t.Errorf("expected type Go, got %s", p.Type)
	}
	if p.Name != "github.com/test/myproject" {
		t.Errorf("expected name github.com/test/myproject, got %s", p.Name)
	}
	if p.TestCommand != "go test ./..." {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
	if p.BuildCommand != "go build ./..." {
		t.Errorf("unexpected BuildCommand: %s", p.BuildCommand)
	}
	if p.Root != tempDir {
		t.Errorf("expected Root %s, got %s", tempDir, p.Root)
	}
}

func TestDetectProject_Node_NPM(t *testing.T) {
	tempDir := t.TempDir()

	// Create package.json
	pkg := `{
  "name": "my-node-app",
  "version": "1.0.0"
}
`
	os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkg), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeNode {
		t.Errorf("expected type Node, got %s", p.Type)
	}
	if p.Name != "my-node-app" {
		t.Errorf("expected name my-node-app, got %s", p.Name)
	}
	if p.PackageManager != "npm" {
		t.Errorf("expected npm, got %s", p.PackageManager)
	}
	if p.TestCommand != "npm test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
}

func TestDetectProject_Node_Yarn(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{"name": "yarn-app"}`), 0644)
	os.WriteFile(filepath.Join(tempDir, "yarn.lock"), []byte(""), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeNode {
		t.Errorf("expected type Node, got %s", p.Type)
	}
	if p.PackageManager != "yarn" {
		t.Errorf("expected yarn, got %s", p.PackageManager)
	}
	if p.TestCommand != "yarn test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
}

func TestDetectProject_Node_PNPM(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{"name": "pnpm-app"}`), 0644)
	os.WriteFile(filepath.Join(tempDir, "pnpm-lock.yaml"), []byte(""), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeNode {
		t.Errorf("expected type Node, got %s", p.Type)
	}
	if p.PackageManager != "pnpm" {
		t.Errorf("expected pnpm, got %s", p.PackageManager)
	}
	if p.TestCommand != "pnpm test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
}

func TestDetectProject_Python_Pyproject(t *testing.T) {
	tempDir := t.TempDir()

	pyproject := `[project]
name = "my-python-pkg"
version = "0.1.0"
`
	os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(pyproject), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypePython {
		t.Errorf("expected type Python, got %s", p.Type)
	}
	if p.Name != "my-python-pkg" {
		t.Errorf("expected name my-python-pkg, got %s", p.Name)
	}
	if p.TestCommand != "pytest" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
}

func TestDetectProject_Python_SetupPy(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "setup.py"), []byte("from setuptools import setup"), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypePython {
		t.Errorf("expected type Python, got %s", p.Type)
	}
}

func TestDetectProject_Python_Requirements(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "requirements.txt"), []byte("flask==2.0.0"), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypePython {
		t.Errorf("expected type Python, got %s", p.Type)
	}
}

func TestDetectProject_Rust(t *testing.T) {
	tempDir := t.TempDir()

	cargo := `[package]
name = "my-rust-app"
version = "0.1.0"
`
	os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte(cargo), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeRust {
		t.Errorf("expected type Rust, got %s", p.Type)
	}
	if p.Name != "my-rust-app" {
		t.Errorf("expected name my-rust-app, got %s", p.Name)
	}
	if p.TestCommand != "cargo test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
	if p.BuildCommand != "cargo build" {
		t.Errorf("unexpected BuildCommand: %s", p.BuildCommand)
	}
}

func TestDetectProject_Java_Maven(t *testing.T) {
	tempDir := t.TempDir()

	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project>
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.example</groupId>
    <artifactId>my-app</artifactId>
    <version>1.0-SNAPSHOT</version>
</project>
`
	os.WriteFile(filepath.Join(tempDir, "pom.xml"), []byte(pom), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeJava {
		t.Errorf("expected type Java, got %s", p.Type)
	}
	if p.TestCommand != "mvn test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
	if p.BuildCommand != "mvn package" {
		t.Errorf("unexpected BuildCommand: %s", p.BuildCommand)
	}
}

func TestDetectProject_Java_Gradle(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "build.gradle"), []byte("apply plugin: 'java'"), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeJava {
		t.Errorf("expected type Java, got %s", p.Type)
	}
	if p.TestCommand != "./gradlew test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
}

func TestDetectProject_Java_GradleKotlin(t *testing.T) {
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "build.gradle.kts"), []byte("plugins { java }"), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeJava {
		t.Errorf("expected type Java, got %s", p.Type)
	}
}

func TestDetectProject_Makefile(t *testing.T) {
	tempDir := t.TempDir()

	makefile := `test:
	go test ./...

build:
	go build ./...

lint:
	golangci-lint run

format:
	gofmt -w .
`
	os.WriteFile(filepath.Join(tempDir, "Makefile"), []byte(makefile), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeMake {
		t.Errorf("expected type Make, got %s", p.Type)
	}
	if p.TestCommand != "make test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
	if p.BuildCommand != "make build" {
		t.Errorf("unexpected BuildCommand: %s", p.BuildCommand)
	}
	if p.LintCommand != "make lint" {
		t.Errorf("unexpected LintCommand: %s", p.LintCommand)
	}
	if p.FormatCommand != "make format" {
		t.Errorf("unexpected FormatCommand: %s", p.FormatCommand)
	}
}

func TestDetectProject_Makefile_Partial(t *testing.T) {
	tempDir := t.TempDir()

	// Makefile with only test target
	makefile := `test:
	pytest
`
	os.WriteFile(filepath.Join(tempDir, "Makefile"), []byte(makefile), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeMake {
		t.Errorf("expected type Make, got %s", p.Type)
	}
	if p.TestCommand != "make test" {
		t.Errorf("unexpected TestCommand: %s", p.TestCommand)
	}
	if p.BuildCommand != "" {
		t.Errorf("expected no BuildCommand, got %s", p.BuildCommand)
	}
}

func TestDetectProject_Unknown(t *testing.T) {
	tempDir := t.TempDir()

	// Empty directory
	p := DetectProject(tempDir)

	if p.Type != ProjectTypeUnknown {
		t.Errorf("expected type Unknown, got %s", p.Type)
	}
	if p.Root != tempDir {
		t.Errorf("expected Root %s, got %s", tempDir, p.Root)
	}
}

func TestDetectProject_Priority(t *testing.T) {
	// When multiple config files exist, Go should take priority
	tempDir := t.TempDir()

	os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module example.com/test"), 0644)
	os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{"name": "test"}`), 0644)
	os.WriteFile(filepath.Join(tempDir, "Makefile"), []byte("test:\n\techo test"), 0644)

	p := DetectProject(tempDir)

	if p.Type != ProjectTypeGo {
		t.Errorf("expected Go to have priority, got %s", p.Type)
	}
}

func TestFileExists(t *testing.T) {
	tempDir := t.TempDir()

	// Create a file
	testFile := filepath.Join(tempDir, "test.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	if !fileExists(testFile) {
		t.Error("expected file to exist")
	}

	if fileExists(filepath.Join(tempDir, "nonexistent.txt")) {
		t.Error("expected file not to exist")
	}
}

func TestMakefileHasTarget(t *testing.T) {
	tempDir := t.TempDir()

	makefile := `build:
	go build

test:
	go test ./...

clean:
	rm -rf bin/
`
	os.WriteFile(filepath.Join(tempDir, "Makefile"), []byte(makefile), 0644)

	tests := []struct {
		target   string
		expected bool
	}{
		{"build", true},
		{"test", true},
		{"clean", true},
		{"lint", false},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		result := makefileHasTarget(tempDir, tt.target)
		if result != tt.expected {
			t.Errorf("makefileHasTarget(%q) = %v, want %v", tt.target, result, tt.expected)
		}
	}
}

func TestGetGoModuleName(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid go.mod", func(t *testing.T) {
		content := `module github.com/user/repo

go 1.21
`
		os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(content), 0644)

		name := getGoModuleName(tempDir)
		if name != "github.com/user/repo" {
			t.Errorf("expected github.com/user/repo, got %s", name)
		}
	})

	t.Run("missing go.mod", func(t *testing.T) {
		emptyDir := t.TempDir()
		name := getGoModuleName(emptyDir)
		if name != "" {
			t.Errorf("expected empty string, got %s", name)
		}
	})
}

func TestGetPackageJsonName(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid package.json", func(t *testing.T) {
		content := `{
  "name": "my-package",
  "version": "1.0.0"
}`
		os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(content), 0644)

		name := getPackageJsonName(tempDir)
		if name != "my-package" {
			t.Errorf("expected my-package, got %s", name)
		}
	})

	t.Run("missing package.json", func(t *testing.T) {
		emptyDir := t.TempDir()
		name := getPackageJsonName(emptyDir)
		if name != "" {
			t.Errorf("expected empty string, got %s", name)
		}
	})
}

func TestGetPyprojectName(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid pyproject.toml", func(t *testing.T) {
		content := `[project]
name = "my-python-pkg"
version = "0.1.0"
`
		os.WriteFile(filepath.Join(tempDir, "pyproject.toml"), []byte(content), 0644)

		name := getPyprojectName(tempDir)
		if name != "my-python-pkg" {
			t.Errorf("expected my-python-pkg, got %s", name)
		}
	})
}

func TestGetCargoName(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid Cargo.toml", func(t *testing.T) {
		content := `[package]
name = "my-rust-app"
version = "0.1.0"
`
		os.WriteFile(filepath.Join(tempDir, "Cargo.toml"), []byte(content), 0644)

		name := getCargoName(tempDir)
		if name != "my-rust-app" {
			t.Errorf("expected my-rust-app, got %s", name)
		}
	})
}
