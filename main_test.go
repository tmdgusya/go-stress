package main

import (
	"strings"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           10,
				Workers:       5,
				Timeout:       5 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty target URL",
			config: Config{
				TargetURL:     "",
				TotalRequests: 100,
				RPS:           10,
				Workers:       5,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "target URL is required",
		},
		{
			name: "zero total requests",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 0,
				RPS:           10,
				Workers:       5,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "total request should be greater than zero",
		},
		{
			name: "negative total requests",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: -10,
				RPS:           10,
				Workers:       5,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "total request should be greater than zero",
		},
		{
			name: "zero timeout",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           10,
				Workers:       5,
				Timeout:       0,
			},
			wantErr: true,
			errMsg:  "timeout must be greater than zero",
		},
		{
			name: "negative timeout",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           10,
				Workers:       5,
				Timeout:       -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "timeout must be greater than zero",
		},
		{
			name: "zero workers",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           10,
				Workers:       0,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "workers must be greater than zero",
		},
		{
			name: "negative workers",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           10,
				Workers:       -5,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "workers must be greater than zero",
		},
		{
			name: "minimal valid config",
			config: Config{
				TargetURL:     "https://api.example.com/endpoint",
				TotalRequests: 1,
				RPS:           1,
				Workers:       1,
				Timeout:       1 * time.Millisecond,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}
