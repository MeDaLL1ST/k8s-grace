package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type requestSpec struct {
	op    string
	kind  string
	delay time.Duration
}

type result struct {
	code int
	err  error
	kind string
}

func main() {
	target := flag.String("target", "http://localhost:8080/work", "target /work URL")
	interval := flag.Duration("interval", 100*time.Millisecond, "interval between launching goroutines")
	mode := flag.String("mode", "mixed", "load mode: mixed or timeout")
	count := flag.Int("count", 50, "number of requests for mixed mode")
	seed := flag.Int64("seed", 0, "random seed; 0 means current time")
	clientTimeout := flag.Duration("client-timeout", 90*time.Second, "HTTP client timeout")
	timeoutDelay := flag.Duration("timeout-delay", 60*time.Second, "delay for timeout mode")
	flag.Parse()

	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(*seed))

	requests := buildRequests(*mode, *count, *timeoutDelay, rng)
	fmt.Printf("load-client mode=%s seed=%d requests=%d interval=%s target=%s\n", *mode, *seed, len(requests), *interval, *target)
	printDistribution(requests)

	var ok atomic.Int64
	var failed atomic.Int64
	var wg sync.WaitGroup
	client := &http.Client{Timeout: *clientTimeout}

	start := time.Now()
	for i, spec := range requests {
		wg.Add(1)
		go func(i int, spec requestSpec) {
			defer wg.Done()
			url := fmt.Sprintf("%s?delay=%s&op=%s", *target, spec.delay, spec.op)
			resp, err := client.Get(url)
			if err != nil {
				failed.Add(1)
				fmt.Printf("%s ERROR kind=%s delay=%s op=%s err=%v\n", time.Since(start).Round(time.Millisecond), spec.kind, spec.delay, spec.op, err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				ok.Add(1)
			} else {
				failed.Add(1)
			}
			fmt.Printf("%s status=%d kind=%s delay=%s op=%s\n", time.Since(start).Round(time.Millisecond), resp.StatusCode, spec.kind, spec.delay, spec.op)
		}(i, spec)
		time.Sleep(*interval)
	}
	wg.Wait()
	fmt.Printf("summary sent=%d ok=%d failed=%d seed=%d mode=%s\n", len(requests), ok.Load(), failed.Load(), *seed, *mode)
}

func buildRequests(mode string, count int, timeoutDelay time.Duration, rng *rand.Rand) []requestSpec {
	if mode == "timeout" {
		return []requestSpec{{op: "long-01", kind: "timeout", delay: timeoutDelay}}
	}
	if count <= 0 {
		count = 50
	}

	// Controlled random distribution: the shares are fixed, but every delay is chosen randomly
	// inside its interval. For the default count=50 this gives 35 short, 10 medium, 5 long requests.
	longCount := count / 10
	mediumCount := count / 5
	shortCount := count - mediumCount - longCount

	requests := make([]requestSpec, 0, count)
	for i := 0; i < shortCount; i++ {
		requests = append(requests, requestSpec{
			op:    fmt.Sprintf("short-%02d", i+1),
			kind:  "short",
			delay: randomDuration(rng, 100*time.Millisecond, 300*time.Millisecond),
		})
	}
	for i := 0; i < mediumCount; i++ {
		requests = append(requests, requestSpec{
			op:    fmt.Sprintf("medium-%02d", i+1),
			kind:  "medium",
			delay: randomDuration(rng, 2*time.Second, 5*time.Second),
		})
	}
	for i := 0; i < longCount; i++ {
		requests = append(requests, requestSpec{
			op:    fmt.Sprintf("long-%02d", i+1),
			kind:  "long",
			delay: randomDuration(rng, 12*time.Second, 18*time.Second),
		})
	}

	rng.Shuffle(len(requests), func(i, j int) {
		requests[i], requests[j] = requests[j], requests[i]
	})
	return requests
}

func randomDuration(rng *rand.Rand, min time.Duration, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	delta := max - min
	return min + time.Duration(rng.Int63n(int64(delta)+1))
}

func printDistribution(requests []requestSpec) {
	var shortCount, mediumCount, longCount, timeoutCount int
	for _, req := range requests {
		switch req.kind {
		case "short":
			shortCount++
		case "medium":
			mediumCount++
		case "long":
			longCount++
		case "timeout":
			timeoutCount++
		}
	}
	fmt.Printf("distribution short=%d medium=%d long=%d timeout=%d\n", shortCount, mediumCount, longCount, timeoutCount)
}
