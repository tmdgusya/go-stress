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
			name:    "default target URL",
			config:  DefaultConfig(),
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

func TestRPSRateLimiting(t *testing.T) {
	tests := []struct {
		name          string
		rps           int
		totalRequests int
		toleranceMs   int // 허용 오차 (밀리초)
	}{
		{
			name:          "10 RPS with 20 requests",
			rps:           10,
			totalRequests: 20,
			toleranceMs:   100, // 100ms 오차 허용
		},
		{
			name:          "100 RPS with 50 requests",
			rps:           100,
			totalRequests: 50,
			toleranceMs:   20,
		},
		{
			name:          "50 RPS with 100 requests",
			rps:           50,
			totalRequests: 100,
			toleranceMs:   30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				RPS:           tt.rps,
				TotalRequests: tt.totalRequests,
			}

			jobs := make(chan Job, 10)
			ticker := time.NewTicker(time.Second / time.Duration(cfg.RPS))
			defer ticker.Stop()

			startTime := time.Now()
			receivedTimes := make([]time.Time, 0, cfg.TotalRequests)

			// Job 생성 고루틴 (실제 코드와 동일한 로직)
			go func() {
				for i := 0; i < cfg.TotalRequests; i++ {
					<-ticker.C
					jobs <- Job{ID: i, URL: "http://test.com"}
				}
				close(jobs)
			}()

			// Job 수신 및 타이밍 기록
			for range jobs {
				receivedTimes = append(receivedTimes, time.Now())
			}

			totalDuration := time.Since(startTime)

			// 예상 소요 시간 계산
			// 첫 번째 요청은 첫 tick 후 바로 발생하므로 (TotalRequests) * interval 소요
			expectedDuration := time.Duration(cfg.TotalRequests) * time.Second / time.Duration(cfg.RPS)
			tolerance := time.Duration(tt.toleranceMs) * time.Millisecond

			// 전체 소요 시간 검증
			if totalDuration < expectedDuration-tolerance || totalDuration > expectedDuration+tolerance {
				t.Errorf("Total duration = %v, expected %v (±%v)",
					totalDuration, expectedDuration, tolerance)
			}

			// 수신된 요청 개수 검증
			if len(receivedTimes) != cfg.TotalRequests {
				t.Errorf("Received %d requests, expected %d",
					len(receivedTimes), cfg.TotalRequests)
			}

			// 개별 요청 간격 검증 (처음 10개만 체크)
			checkCount := 10
			if len(receivedTimes) < checkCount {
				checkCount = len(receivedTimes)
			}

			expectedInterval := time.Second / time.Duration(cfg.RPS)
			for i := 1; i < checkCount; i++ {
				interval := receivedTimes[i].Sub(receivedTimes[i-1])

				// 각 간격이 예상 간격의 ±tolerance 범위 내인지 확인
				if interval < expectedInterval-tolerance || interval > expectedInterval+tolerance {
					t.Errorf("Request %d interval = %v, expected %v (±%v)",
						i, interval, expectedInterval, tolerance)
				}
			}

			t.Logf("✓ RPS Test passed: %d requests in %v (expected ~%v)",
				len(receivedTimes), totalDuration, expectedDuration)
		})
	}
}

func TestRPSActualRate(t *testing.T) {
	// 실제 RPS 측정 테스트
	cfg := Config{
		RPS:           100,
		TotalRequests: 200,
	}

	jobs := make(chan Job, 10)
	ticker := time.NewTicker(time.Second / time.Duration(cfg.RPS))
	defer ticker.Stop()

	startTime := time.Now()
	count := 0

	go func() {
		for i := 0; i < cfg.TotalRequests; i++ {
			<-ticker.C
			jobs <- Job{ID: i, URL: "http://test.com"}
		}
		close(jobs)
	}()

	for range jobs {
		count++
	}

	duration := time.Since(startTime)
	actualRPS := float64(count) / duration.Seconds()

	// 실제 RPS가 설정된 RPS의 ±10% 범위 내인지 확인
	expectedRPS := float64(cfg.RPS)
	lowerBound := expectedRPS * 0.9
	upperBound := expectedRPS * 1.1

	if actualRPS < lowerBound || actualRPS > upperBound {
		t.Errorf("Actual RPS = %.2f, expected %.2f (±10%%)", actualRPS, expectedRPS)
	}

	t.Logf("✓ Actual RPS: %.2f (target: %.2f, requests: %d, duration: %v)",
		actualRPS, expectedRPS, count, duration)
}
