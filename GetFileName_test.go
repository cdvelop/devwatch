package devwatch

import "testing"

func TestGetFileName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
		wantErr  bool
	}{
		{
			name:     "valid path",
			path:     "theme/index.html",
			expected: "index.html",
			wantErr:  false,
		},
		{
			name:     "valid path backslash",
			path:     "theme\\index.html",
			expected: "index.html",
			wantErr:  false,
		},
		{
			name:     "only filename",
			path:     "index.html",
			expected: "index.html",
			wantErr:  false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "path ends in separator",
			path:     "theme/",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "path is separator",
			path:     "/",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "path is current dir",
			path:     ".",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetFileName(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetFileName(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Fatalf("GetFileName(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}
