package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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
			name:    "default target URL is allowed",
			config:  DefaultConfig(),
			wantErr: false,
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
			name: "zero rps",
			config: Config{
				TargetURL:     "http://example.com",
				TotalRequests: 100,
				RPS:           0,
				Workers:       5,
				Timeout:       5 * time.Second,
			},
			wantErr: true,
			errMsg:  "rps must be greater than zero",
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

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	expected := Config{
		TargetURL:     "http://127.0.0.1:3000",
		TotalRequests: 100,
		RPS:           20,
		Workers:       10,
		Timeout:       5 * time.Second,
	}

	if cfg != expected {
		t.Errorf("DefaultConfig() = %+v, want %+v", cfg, expected)
	}
}

func TestPercentile(t *testing.T) {
	latencies := make([]time.Duration, 100)
	for i := range latencies {
		latencies[i] = time.Duration(i+1) * time.Millisecond
	}

	tests := []struct {
		name      string
		latencies []time.Duration
		p         float64
		want      time.Duration
	}{
		{"empty", []time.Duration{}, 0.5, 0},
		{"single", []time.Duration{100 * time.Millisecond}, 0.9, 100 * time.Millisecond},
		{"p50", latencies[:10], 0.5, 5 * time.Millisecond},
		{"p90", latencies[:10], 0.9, 9 * time.Millisecond},
		{"p99", latencies[:10], 0.99, 9 * time.Millisecond},
		{"p99 with 100", latencies, 0.99, 99 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.latencies, tt.p)
			if got != tt.want {
				t.Errorf("percentile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateSummary(t *testing.T) {
	const ms = time.Millisecond
	tests := []struct {
		name        string
		latencies   []time.Duration
		statusCodes map[int]int
		total       int
		failures    int
		want        Summary
	}{
		{
			name:        "normal case",
			latencies:   []time.Duration{100 * ms, 200 * ms, 300 * ms, 400 * ms, 500 * ms, 600 * ms, 700 * ms, 800 * ms, 900 * ms, 1000 * ms},
			statusCodes: map[int]int{200: 10},
			total:       10,
			failures:    0,
			want: Summary{
				TotalRequests: 10,
				Success:       10,
				Failures:      0,
				MinLatency:    100 * ms,
				MaxLatency:    1000 * ms,
				AvgLatency:    550 * ms,
				P50:           500 * ms,
				P90:           900 * ms,
				P99:           900 * ms,
				StatusCodes:   map[int]int{200: 10},
			},
		},
		{
			name:        "with failures",
			latencies:   []time.Duration{100 * ms, 200 * ms},
			statusCodes: map[int]int{200: 2, 500: 1},
			total:       3,
			failures:    1,
			want: Summary{
				TotalRequests: 3,
				Success:       2,
				Failures:      1,
				MinLatency:    100 * ms,
				MaxLatency:    200 * ms,
				AvgLatency:    150 * ms,
				P50:           100 * ms,
				P90:           100 * ms,
				P99:           100 * ms,
				StatusCodes:   map[int]int{200: 2, 500: 1},
			},
		},
		{
			name:        "all fail",
			latencies:   []time.Duration{},
			statusCodes: map[int]int{},
			total:       5,
			failures:    5,
			want: Summary{
				TotalRequests: 5,
				Success:       0,
				Failures:      5,
				MinLatency:    0,
				MaxLatency:    0,
				AvgLatency:    0,
				P50:           0,
				P90:           0,
				P99:           0,
				StatusCodes:   map[int]int{},
			},
		},
		{
			name:        "single success",
			latencies:   []time.Duration{123 * time.Millisecond},
			statusCodes: map[int]int{200: 1},
			total:       1,
			failures:    0,
			want: Summary{
				TotalRequests: 1,
				Success:       1,
				Failures:      0,
				MinLatency:    123 * ms,
				MaxLatency:    123 * ms,
				AvgLatency:    123 * ms,
				P50:           123 * ms,
				P90:           123 * ms,
				P99:           123 * ms,
				StatusCodes:   map[int]int{200: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSummary(tt.latencies, tt.statusCodes, tt.total, tt.failures)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateSummary() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAggregator(t *testing.T) {
	resultsCh := make(chan Result, 5)
	summaryCh := make(chan Summary, 1)

	results := []Result{
		{Latency: 100 * time.Millisecond, StatusCode: 200},
		{Latency: 200 * time.Millisecond, StatusCode: 200},
		{Latency: 300 * time.Millisecond, StatusCode: 404},
		{Err: fmt.Errorf("network error")},
	}

	go func() {
		for _, r := range results {
			resultsCh <- r
		}
		close(resultsCh)
	}()

	aggregator(resultsCh, summaryCh)
	summary := <-summaryCh

	expectedSummary := Summary{
		TotalRequests: 4,
		Success:       3,
		Failures:      1,
		MinLatency:    100 * time.Millisecond,
		MaxLatency:    300 * time.Millisecond,
		AvgLatency:    200 * time.Millisecond,
		P50:           200 * time.Millisecond,
		P90:           200 * time.Millisecond,
		P99:           200 * time.Millisecond,
		StatusCodes:   map[int]int{200: 2, 404: 1},
	}

	if !reflect.DeepEqual(summary, expectedSummary) {
		t.Errorf("aggregator() summary = %+v, want %+v", summary, expectedSummary)
	}
}

func TestWorker(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		jobs := make(chan Job, 1)
		results := make(chan Result, 1)
		jobs <- Job{ID: 1, URL: server.URL}
		close(jobs)

		client := newHTTPClient(Config{Timeout: 1 * time.Second, Workers: 1})

		worker(context.Background(), 1, client, jobs, results)

		res := <-results
		if res.Err != nil {
			t.Fatalf("worker() unexpected error: %v", res.Err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("worker() status = %d, want %d", res.StatusCode, http.StatusOK)
		}
	})

	t.Run("request timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		jobs := make(chan Job, 1)
		results := make(chan Result, 1)
		jobs <- Job{ID: 1, URL: server.URL}
		close(jobs)

		client := newHTTPClient(Config{Timeout: 50 * time.Millisecond, Workers: 1})

		worker(context.Background(), 1, client, jobs, results)

		res := <-results
		if res.Err == nil {
			t.Fatal("worker() expected a timeout error, but got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond) // Ensure the request doesn't finish
		}))
		defer server.Close()

		jobs := make(chan Job, 1)
		results := make(chan Result, 1)
		ctx, cancel := context.WithCancel(context.Background())

		jobs <- Job{ID: 1, URL: server.URL}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := newHTTPClient(Config{Timeout: 1 * time.Second, Workers: 1})
			worker(ctx, 1, client, jobs, results)
		}()

		time.Sleep(50 * time.Millisecond) // Give the worker time to start
		cancel()
		wg.Wait()
		close(results)

		// Check that no result was sent after cancellation
		if _, ok := <-results; ok {
			t.Error("worker() sent a result after context cancellation")
		}
	})
}

func TestRunLoadTest(t *testing.T) {
	t.Run("successful run with mixed status codes", func(t *testing.T) {
		var requestCounter int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCounter, 1)
			if count%5 == 0 {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		cfg := Config{
			TargetURL:     server.URL,
			TotalRequests: 100,
			RPS:           200, // High RPS to finish quickly
			Workers:       20,
			Timeout:       1 * time.Second,
		}

		summary, err := RunLoadTest(context.Background(), cfg)
		if err != nil {
			t.Fatalf("RunLoadTest() returned an unexpected error: %v", err)
		}

		if summary.TotalRequests != cfg.TotalRequests {
			t.Errorf("summary.TotalRequests = %d, want %d", summary.TotalRequests, cfg.TotalRequests)
		}
		if summary.Failures != 0 {
			t.Errorf("summary.Failures = %d, want 0", summary.Failures)
		}
		if summary.Success != cfg.TotalRequests {
			t.Errorf("summary.Success = %d, want %d", summary.Success, cfg.TotalRequests)
		}

		expectedOK := 80
		expectedErr := 20
		if summary.StatusCodes[http.StatusOK] != expectedOK {
			t.Errorf("summary.StatusCodes[200] = %d, want %d", summary.StatusCodes[http.StatusOK], expectedOK)
		}
		if summary.StatusCodes[http.StatusInternalServerError] != expectedErr {
			t.Errorf("summary.StatusCodes[500] = %d, want %d", summary.StatusCodes[http.StatusInternalServerError], expectedErr)
		}
	})

	t.Run("run with timeouts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()

		cfg := Config{
			TargetURL:     server.URL,
			TotalRequests: 20,
			RPS:           100,
			Workers:       10,
			Timeout:       50 * time.Millisecond, // Timeout is less than server response time
		}

		summary, err := RunLoadTest(context.Background(), cfg)
		if err != nil {
			t.Fatalf("RunLoadTest() returned an unexpected error: %v", err)
		}

		if summary.TotalRequests != cfg.TotalRequests {
			t.Errorf("summary.TotalRequests = %d, want %d", summary.TotalRequests, cfg.TotalRequests)
		}
		if summary.Failures != cfg.TotalRequests {
			t.Errorf("summary.Failures = %d, want %d", summary.Failures, cfg.TotalRequests)
		}
		if summary.Success != 0 {
			t.Errorf("summary.Success = %d, want 0", summary.Success)
		}
	})
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
