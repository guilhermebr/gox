package supabase

import (
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		envVars map[string]string
		wantErr bool
	}{
		{
			name:   "Valid configuration",
			prefix: "APP",
			envVars: map[string]string{
				"APP_SUPABASE_URL": "https://test.supabase.co",
				"APP_SUPABASE_KEY": "test-key",
			},
			wantErr: false,
		},
		{
			name:   "Missing URL",
			prefix: "APP",
			envVars: map[string]string{
				"APP_SUPABASE_KEY": "test-key",
			},
			wantErr: true,
		},
		{
			name:   "Missing Key",
			prefix: "APP",
			envVars: map[string]string{
				"APP_SUPABASE_URL": "https://test.supabase.co",
			},
			wantErr: true,
		},
		{
			name:    "Missing both URL and Key",
			prefix:  "APP",
			envVars: map[string]string{},
			wantErr: true,
		},
		{
			name:   "Invalid URL format",
			prefix: "APP",
			envVars: map[string]string{
				"APP_SUPABASE_URL": "invalid-url",
				"APP_SUPABASE_KEY": "test-key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv sets the variable for the test and restores it on cleanup.
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			client, err := New(tt.prefix)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create supabase client: %v", err)
			}

			if client == nil {
				t.Fatal("Client should not be null")
			}
		})
	}
}

func TestNewFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "Valid configuration",
			config: Config{
				URL: "https://test.supabase.co",
				Key: "test-key",
			},
			wantErr: false,
		},
		{
			name: "Empty URL",
			config: Config{
				URL: "",
				Key: "test-key",
			},
			wantErr: true,
		},
		{
			name: "Empty Key",
			config: Config{
				URL: "https://test.supabase.co",
				Key: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewFromConfig(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to create supabase client: %v", err)
			}

			if client == nil {
				t.Fatal("Client should not be null")
			}
		})
	}
}
