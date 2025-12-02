package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

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

func DefaultConfig(url string) Config {
	return Config{
		TargetURL:     url,
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

	return nil
}

func worker(ctx context.Context, id int, jobs <-chan Job, results chan<- Result) {
	client := &http.Client{}

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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerCount := 10
	totalRequests := 100
	rps := 20 // 초당 요청 수 제한

	jobs := make(chan Job, 100)
	results := make(chan Result, 100)

	// worker 생성
	for w := 1; w <= workerCount; w++ {
		go worker(ctx, w, jobs, results)
	}

	// aggregator
	var aggWG sync.WaitGroup
	aggWG.Add(1)
	go aggregator(ctx, &aggWG, results)

	// rate limiter
	ticker := time.NewTicker(time.Second / time.Duration(rps))

	// job producer
	go func() {
		for i := 0; i < totalRequests; i++ {
			<-ticker.C
			jobs <- Job{ID: i, URL: "https://google.com"}
		}
		close(jobs)
	}()

	// 대기
	time.Sleep(5 * time.Second)
	close(results)
	aggWG.Wait()

	fmt.Println("Done.")
}
