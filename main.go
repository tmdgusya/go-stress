package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
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
	if c.TargetURL == "" || c.TargetURL == DEFAULT_TARGET_URL {
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

	return nil
}

func worker(ctx context.Context, id int, jobs <-chan Job, results chan<- Result, timeout time.Duration) {
	client := &http.Client{
		Timeout: timeout,
	}

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			start := time.Now()
			resp, err := client.Get(job.URL)
			defer resp.Body.Close()

			latency := time.Since(start)

			if err != nil {

				continue
			}

			results <- Result{
				JobID:      job.ID,
				StatusCode: resp.StatusCode,
				Latency:    latency,
				Err:        nil,
			}
		}
	}
}

func aggregator(ctx context.Context, wg *sync.WaitGroup, results <-chan Result) {
	defer wg.Done()

	var count int
	var sumLatency time.Duration
	var errors int

	for {
		select {
		case <-ctx.Done():
			return
		case res, ok := <-results:
			if !ok {
				return
			}

			count++

			if res.Err != nil {
				errors++
			} else {
				sumLatency += res.Latency
			}
		}
	}
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

func main() {
	cfg, err := ParseFlags()
	if err != nil {
		panic(err)
	}

	fmt.Println("Running load test with config: ", cfg)

	err = RunLoadTest(cfg)
	if err != nil {
		panic(err)
	}

	fmt.Println("Load test completed")
}

func RunLoadTest(cfg Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan Job, cfg.Workers*2)
	results := make(chan Result, cfg.Workers*2)

	for w := 1; w <= cfg.Workers; w++ {
		go worker(ctx, w, jobs, results, cfg.Timeout)
	}

	var aggWg sync.WaitGroup
	aggWg.Add(1)
	go aggregator(ctx, &aggWg, results)

	ticker := time.NewTicker(time.Second / time.Duration(cfg.RPS))

	go func() {
		for i := 0; i < cfg.TotalRequests; i++ {
			<-ticker.C
			jobs <- Job{ID: i, URL: cfg.TargetURL}
		}
		close(jobs)
	}()

	time.Sleep(time.Duration(cfg.TotalRequests/cfg.RPS+3) * time.Second)
	close(results)
	aggWg.Wait()

	return nil
}
