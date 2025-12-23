package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewVerifier(t *testing.T) {
	p := &Project{
		Type:        ProjectTypeGo,
		Root:        "/test",
		TestCommand: "go test ./...",
	}

	v := NewVerifier(p)
	if v.project != p {
		t.Error("expected project to be set")
	}
}

func TestVerifier_RunTests_NoCommand(t *testing.T) {
	p := &Project{
		Type:        ProjectTypeUnknown,
		TestCommand: "",
	}
	v := NewVerifier(p)

	result := v.RunTests(context.Background())
	if !result.Success {
		t.Error("expected success when no command")
	}
	if result.Output != "No test command configured for this project type" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_RunBuild_NoCommand(t *testing.T) {
	p := &Project{
		Type:         ProjectTypeUnknown,
		BuildCommand: "",
	}
	v := NewVerifier(p)

	result := v.RunBuild(context.Background())
	if !result.Success {
		t.Error("expected success when no command")
	}
	if result.Output != "No build command configured for this project type" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_RunLint_NoCommand(t *testing.T) {
	p := &Project{
		Type:        ProjectTypeUnknown,
		LintCommand: "",
	}
	v := NewVerifier(p)

	result := v.RunLint(context.Background())
	if !result.Success {
		t.Error("expected success when no command")
	}
	if result.Output != "No lint command configured for this project type" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_RunFormat_NoCommand(t *testing.T) {
	p := &Project{
		Type:          ProjectTypeUnknown,
		FormatCommand: "",
	}
	v := NewVerifier(p)

	result := v.RunFormat(context.Background())
	if !result.Success {
		t.Error("expected success when no command")
	}
	if result.Output != "No format command configured for this project type" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_RunTests_Success(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "echo 'tests passed'",
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := v.RunTests(ctx)
	if !result.Success {
		t.Errorf("expected success, got failure with output: %s", result.Output)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Command != "echo 'tests passed'" {
		t.Errorf("expected command to be set, got %s", result.Command)
	}
}

func TestVerifier_RunTests_Failure(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "exit 1",
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := v.RunTests(ctx)
	if result.Success {
		t.Error("expected failure")
	}
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
}

func TestVerifier_RunAll(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:          ProjectTypeMake,
		Root:          tempDir,
		TestCommand:   "echo test",
		BuildCommand:  "echo build",
		LintCommand:   "echo lint",
		FormatCommand: "echo format",
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := v.RunAll(ctx)

	// Should run all 4 commands: format, lint, build, test
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Check order: format, lint, build, test
	expectedOrder := []string{"echo format", "echo lint", "echo build", "echo test"}
	for i, expected := range expectedOrder {
		if results[i].Command != expected {
			t.Errorf("results[%d].Command = %s, want %s", i, results[i].Command, expected)
		}
		if !results[i].Success {
			t.Errorf("results[%d] should succeed", i)
		}
	}
}

func TestVerifier_RunAll_Partial(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "echo test",
		// No build, lint, format commands
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := v.RunAll(ctx)

	// Should only run test command
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Command != "echo test" {
		t.Errorf("expected test command, got %s", results[0].Command)
	}
}

func TestVerifier_QuickCheck_Go(t *testing.T) {
	tempDir := t.TempDir()

	// Create minimal Go project
	os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module test\ngo 1.21"), 0644)
	os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\nfunc main() {}"), 0644)

	p := &Project{
		Type: ProjectTypeGo,
		Root: tempDir,
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := v.QuickCheck(ctx)
	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Output)
	}
}

func TestVerifier_QuickCheck_Unknown(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type: ProjectTypeUnknown,
		Root: tempDir,
	}
	v := NewVerifier(p)

	result := v.QuickCheck(context.Background())
	if !result.Success {
		t.Error("expected success for unknown project type")
	}
	if result.Output != "No quick check available for unknown project type" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_QuickCheck_Make_NoTest(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "", // No test command
	}
	v := NewVerifier(p)

	result := v.QuickCheck(context.Background())
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "No quick check available" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestVerifier_StderrCapture(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "echo 'stdout' && echo 'stderr' >&2",
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := v.RunTests(ctx)
	if !result.Success {
		t.Errorf("expected success, got failure")
	}

	// Both stdout and stderr should be captured
	if result.Output == "" {
		t.Error("expected output to be captured")
	}
}

func TestVerifier_Duration(t *testing.T) {
	tempDir := t.TempDir()

	p := &Project{
		Type:        ProjectTypeMake,
		Root:        tempDir,
		TestCommand: "sleep 0.1",
	}
	v := NewVerifier(p)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := v.RunTests(ctx)
	if result.Duration < 100*time.Millisecond {
		t.Errorf("expected duration >= 100ms, got %v", result.Duration)
	}
}

func TestGetFailingTests_Go(t *testing.T) {
	output := `=== RUN   TestExample
--- FAIL: TestExample (0.00s)
    example_test.go:10: expected 1, got 2
=== RUN   TestAnother
--- PASS: TestAnother (0.00s)
=== RUN   TestBroken
--- FAIL: TestBroken (0.00s)
    broken_test.go:5: assertion failed
FAIL`

	failing := GetFailingTests(output, ProjectTypeGo)

	if len(failing) != 2 {
		t.Fatalf("expected 2 failing tests, got %d: %v", len(failing), failing)
	}
	if failing[0] != "TestExample" {
		t.Errorf("expected first failure TestExample, got %s", failing[0])
	}
	if failing[1] != "TestBroken" {
		t.Errorf("expected second failure TestBroken, got %s", failing[1])
	}
}

func TestGetFailingTests_Node(t *testing.T) {
	output := `PASS src/utils.test.js
FAIL src/api.test.js
  ✕ should handle errors (5ms)
  ✓ should return data
FAIL src/auth.test.js`

	failing := GetFailingTests(output, ProjectTypeNode)

	if len(failing) < 2 {
		t.Fatalf("expected at least 2 failing items, got %d: %v", len(failing), failing)
	}
}

func TestGetFailingTests_Python(t *testing.T) {
	// Pytest output format: "FAILED test_file.py::test_name - reason"
	output := `test_example.py::test_one PASSED
FAILED test_example.py::test_two - AssertionError
test_example.py::test_three PASSED
FAILED test_api.py::test_request - ConnectionError`

	failing := GetFailingTests(output, ProjectTypePython)

	if len(failing) != 2 {
		t.Fatalf("expected 2 failing tests, got %d: %v", len(failing), failing)
	}
	if failing[0] != "test_example.py::test_two" {
		t.Errorf("expected first failure test_example.py::test_two, got %s", failing[0])
	}
}

func TestGetFailingTests_Rust(t *testing.T) {
	output := `running 3 tests
test utils::tests::test_parse ... ok
test api::tests::test_request ... FAILED
test auth::tests::test_login ... FAILED

failures:
    api::tests::test_request
    auth::tests::test_login`

	failing := GetFailingTests(output, ProjectTypeRust)

	if len(failing) != 2 {
		t.Fatalf("expected 2 failing tests, got %d: %v", len(failing), failing)
	}
}

func TestGetFailingTests_Unknown(t *testing.T) {
	output := `Some random output`

	failing := GetFailingTests(output, ProjectTypeUnknown)

	if len(failing) != 0 {
		t.Errorf("expected 0 failing tests for unknown type, got %d", len(failing))
	}
}
