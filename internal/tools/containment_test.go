package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePathContainment(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		root    string
		path    string
		wantErr bool
	}{
		{
			name:    "relative path within root",
			root:    tempDir,
			path:    "subdir/file.txt",
			wantErr: false,
		},
		{
			name:    "relative path at root",
			root:    tempDir,
			path:    "file.txt",
			wantErr: false,
		},
		{
			name:    "absolute path within root",
			root:    tempDir,
			path:    filepath.Join(tempDir, "subdir", "file.txt"),
			wantErr: false,
		},
		{
			name:    "relative path traversal blocked",
			root:    tempDir,
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path outside root blocked",
			root:    tempDir,
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "sneaky traversal with valid prefix",
			root:    tempDir,
			path:    "subdir/../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "root path itself is allowed",
			root:    tempDir,
			path:    tempDir,
			wantErr: false,
		},
		{
			name:    "dot path is allowed",
			root:    tempDir,
			path:    ".",
			wantErr: false,
		},
		{
			name:    "double dot from subdir",
			root:    tempDir,
			path:    "subdir/..",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathContainment(tt.root, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePathContainment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	tempDir := t.TempDir()

	// Create a subdirectory for testing
	subDir := filepath.Join(tempDir, "subdir")
	os.MkdirAll(subDir, 0755)

	tests := []struct {
		name    string
		root    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name:    "simple relative path",
			root:    tempDir,
			path:    "file.txt",
			want:    filepath.Join(tempDir, "file.txt"),
			wantErr: false,
		},
		{
			name:    "nested relative path",
			root:    tempDir,
			path:    "subdir/file.txt",
			want:    filepath.Join(tempDir, "subdir", "file.txt"),
			wantErr: false,
		},
		{
			name:    "absolute path within root",
			root:    tempDir,
			path:    filepath.Join(tempDir, "file.txt"),
			want:    filepath.Join(tempDir, "file.txt"),
			wantErr: false,
		},
		{
			name:    "traversal attack blocked",
			root:    tempDir,
			path:    "../../../etc/passwd",
			want:    "",
			wantErr: true,
		},
		{
			name:    "absolute path outside root blocked",
			root:    tempDir,
			path:    "/tmp/other/file.txt",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePath(tt.root, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolvePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("resolvePath() = %v, want %v", got, tt.want)
			}
		})
	}
}
