package src

import (
	"crypto/md5"
	"encoding/hex"
	"testing"
)

func TestResolveHostIPNoPanic(t *testing.T) {
	// Backup original values
	originalSettings := Settings
	originalSystem := System
	defer func() {
		Settings = originalSettings
		System = originalSystem
	}()

	// Reset System and Settings to a clean state for the test
	System = SystemStruct{}
	Settings = SettingsStruct{}

	// This should not panic
	err := resolveHostIP()
	if err != nil {
		t.Fatalf("resolveHostIP() returned an error: %v", err)
	}
}

func TestRandomString(t *testing.T) {
	tests := []struct {
		name      string
		length    int
		wantError bool
	}{
		{"Length 0", 0, false},
		{"Length 1", 1, false},
		{"Length 10", 10, false},
		{"Length 100", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := randomString(tt.length)
			if (err != nil) != tt.wantError {
				t.Errorf("randomString() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if len(got) != tt.length {
				t.Errorf("randomString() length = %v, want %v", len(got), tt.length)
			}
			// Verify character set
			for _, char := range got {
				if !isAlphanumeric(char) {
					t.Errorf("randomString() contains invalid character: %c", char)
				}
			}
		})
	}
}

func isAlphanumeric(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func TestRandomStringUniqueness(t *testing.T) {
	n := 10
	s1, err := randomString(n)
	if err != nil {
		t.Fatalf("randomString failed: %v", err)
	}
	s2, err := randomString(n)
	if err != nil {
		t.Fatalf("randomString failed: %v", err)
	}

	if s1 == s2 {
		t.Errorf("randomString generated duplicate strings: %s", s1)
	}
}

func TestGetMD5(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "Empty string",
			input: "",
			want:  "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			name:  "Hello World",
			input: "Hello World",
			want:  "b10a8db164e0754105b7a99be72e3fe5",
		},
		{
			name:  "xTeVe",
			input: "xTeVe",
			want:  "82ec32baa5db5981cbd3f8e48008c330", // echo -n "xTeVe" | md5sum
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getMD5(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getMD5() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getMD5() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBindToStruct(t *testing.T) {
	type TargetStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	tests := []struct {
		name    string
		input   any
		want    TargetStruct
		wantErr bool
	}{
		{
			name: "Map input",
			input: map[string]any{
				"name": "Teddy",
				"age":  5,
			},
			want: TargetStruct{Name: "Teddy", Age: 5},
		},
		{
			name: "Struct input",
			input: struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}{Name: "Bear", Age: 3},
			want: TargetStruct{Name: "Bear", Age: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got TargetStruct
			err := bindToStruct(tt.input, &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("bindToStruct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("bindToStruct() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		data    map[string]any
		want    string
	}{
		{
			name:    "Simple substitution",
			content: "Hello {{.Name}}",
			data:    map[string]any{"Name": "Teddy"},
			want:    "Hello Teddy",
		},
		{
			name:    "No substitution",
			content: "Hello World",
			data:    map[string]any{},
			want:    "Hello World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTemplate(tt.content, tt.data)
			if got != tt.want {
				t.Errorf("parseTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper to calculate MD5 for verification
func calculateMD5(s string) string {
	hash := md5.Sum([]byte(s))
	return hex.EncodeToString(hash[:])
}
