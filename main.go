package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

const DEFAULT_TARGET_URL = "http://127.0.0.1:3000"

type Job struct {
	ID  int
	URL string
}

type Result struct {
	JobID      int
	StatusCode int
	Latency    time.Duration
	Err        error
}

type Config struct {
	TargetURL     string        // url to test
	TotalRequests int           // total number of requests to send
	RPS           int           // request per second
	Workers       int           // worker pool size
	Timeout       time.Duration // HTTP request timeout
}

type Summary struct {
	TotalRequests int
	Success       int
	Failures      int

	MinLatency time.Duration
	MaxLatency time.Duration
	AvgLatency time.Duration
	P50        time.Duration
	P90        time.Duration
	P99        time.Duration

	StatusCodes map[int]int
}

func DefaultConfig() Config {
	return Config{
		TargetURL:     DEFAULT_TARGET_URL,
		TotalRequests: 100,
		RPS:           20,
		Workers:       10,
		Timeout:       5 * time.Second,
	}
}

func (c *Config) Validate() error {
	if c.TargetURL == "" {
		return fmt.Errorf("target URL is required")
	}

	if c.TotalRequests <= 0 {
		return fmt.Errorf("total request should be greater than zero")
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}

	if c.Workers <= 0 {
		return fmt.Errorf("workers must be greater than zero")
	}

	if c.RPS <= 0 {
		return fmt.Errorf("rps must be greater than zero")
	}

	return nil
}

func newHTTPClient(cfg Config) *http.Client {
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.Workers * 2,
			MaxIdleConnsPerHost: cfg.Workers * 2,
			MaxConnsPerHost:     cfg.Workers * 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func worker(ctx context.Context, id int, client *http.Client, jobs <-chan Job, results chan<- Result) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", job.URL, nil)
			if err != nil {
				results <- Result{
					JobID: job.ID,
					Err:   err,
				}
				continue
			}

			start := time.Now()
			resp, err := client.Do(req)
			latency := time.Since(start)

			if err != nil {
				// The error is likely a context cancellation or timeout.
				// We still report it as a failed job.
				// The test will be adjusted to expect this.
				// However, if the context is canceled, we should not send a result.
				select {
				case <-ctx.Done():
					return
				default:
					results <- Result{
						JobID:   job.ID,
						Latency: latency,
						Err:     err,
					}
				}
				continue
			}

			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			results <- Result{
				JobID:      job.ID,
				StatusCode: resp.StatusCode,
				Latency:    latency,
				Err:        nil,
			}
		}
	}
}

func percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	idx := int(float64(len(latencies)-1) * p)
	return latencies[idx]
}

func calculateSummary(latencies []time.Duration, statusCodes map[int]int, total, failures int) Summary {
	sum := time.Duration(0)
	min := time.Duration(0)
	max := time.Duration(0)

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		min = latencies[0]
		max = latencies[len(latencies)-1]
		for _, l := range latencies {
			sum += l
		}
	}

	success := total - failures
	var avg, p50, p90, p99 time.Duration

	if success > 0 {
		avg = sum / time.Duration(success)
		p50 = percentile(latencies, 0.50)
		p90 = percentile(latencies, 0.90)
		p99 = percentile(latencies, 0.99)
	}

	return Summary{
		TotalRequests: total,
		Success:       success,
		Failures:      failures,
		MinLatency:    min,
		MaxLatency:    max,
		AvgLatency:    avg,
		P50:           p50,
		P90:           p90,
		P99:           p99,
		StatusCodes:   statusCodes,
	}
}

func aggregator(results <-chan Result, summaryCh chan<- Summary) {
	var total int
	var failures int
	statusCodes := make(map[int]int)
	var latencies []time.Duration

	for res := range results {
		total++

		if res.Err != nil {
			failures++
			continue
		}

		statusCodes[res.StatusCode]++
		latencies = append(latencies, res.Latency)
	}

	summary := calculateSummary(latencies, statusCodes, total, failures)
	summaryCh <- summary
	close(summaryCh)
}

func ParseFlags() (Config, error) {
	cfg := DefaultConfig()

	flag.StringVar(&cfg.TargetURL, "url", cfg.TargetURL, "Target URL to test")
	flag.IntVar(&cfg.TotalRequests, "n", cfg.TotalRequests, "Total number of requests")
	flag.IntVar(&cfg.RPS, "rps", cfg.RPS, "requests per second")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "Number of workers")
	flag.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP timeout")

	flag.Parse()

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func printSummary(cfg Config, s Summary) {
	fmt.Println("========== Load Test Result ==========")
	fmt.Println("Target URL     :", cfg.TargetURL)
	fmt.Println("Total Requests :", s.TotalRequests)
	fmt.Println("Success        :", s.Success)
	fmt.Println("Failures       :", s.Failures)

	if s.Success > 0 {
		fmt.Println("Min Latency    :", s.MinLatency)
		fmt.Println("Max Latency    :", s.MaxLatency)
		fmt.Println("Avg Latency    :", s.AvgLatency)
		fmt.Println("P50 Latency    :", s.P50)
		fmt.Println("P90 Latency    :", s.P90)
		fmt.Println("P99 Latency    :", s.P99)
	}

	fmt.Println("Status Codes:")
	for code, count := range s.StatusCodes {
		fmt.Printf("  %d: %d\n", code, count)
	}
	fmt.Println("======================================")
}

func RunLoadTest(ctx context.Context, cfg Config) (Summary, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Job, cfg.TotalRequests)
	results := make(chan Result, cfg.Workers*2)
	summaryCh := make(chan Summary, 1)

	go aggregator(results, summaryCh)

	client := newHTTPClient(cfg)

	// worker pool
	var wg sync.WaitGroup
	for w := 1; w <= cfg.Workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(ctx, id, client, jobs, results)
		}(w)
	}

	ticker := time.NewTicker(time.Second / time.Duration(cfg.RPS))
	defer ticker.Stop()

	// job producer
	go func() {
		defer close(jobs)
		for i := 0; i < cfg.TotalRequests; i++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				jobs <- Job{ID: i, URL: cfg.TargetURL}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	summary := <-summaryCh
	return summary, nil
}

func main() {
	cfg, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	fmt.Println("Running load test with config:", cfg)

	summary, err := RunLoadTest(context.Background(), cfg)
	if err != nil {
		panic(err)
	}

	printSummary(cfg, summary)
}
