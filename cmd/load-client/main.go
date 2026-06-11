package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	code int
	err  error
}

func main() {
	target := flag.String("target", "http://localhost:8080/work", "target /work URL")
	interval := flag.Duration("interval", 100*time.Millisecond, "interval between launching requests")
	flag.Parse()

	delays := make([]time.Duration, 0, 50)
	for i := 0; i < 35; i++ {
		delays = append(delays, time.Duration(100+i%3*100)*time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		delays = append(delays, time.Duration(2+i%4)*time.Second)
	}
	for i := 0; i < 5; i++ {
		delays = append(delays, time.Duration(12+i%7)*time.Second)
	}

	var ok atomic.Int64
	var failed atomic.Int64
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 45 * time.Second}

	start := time.Now()
	for i, delay := range delays {
		wg.Add(1)
		go func(i int, delay time.Duration) {
			defer wg.Done()
			url := fmt.Sprintf("%s?delay=%s&op=req-%02d", *target, delay, i+1)
			resp, err := client.Get(url)
			if err != nil {
				failed.Add(1)
				fmt.Printf("%s ERROR %s\n", time.Since(start).Round(time.Millisecond), err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				ok.Add(1)
			} else {
				failed.Add(1)
			}
			fmt.Printf("%s status=%d delay=%s op=req-%02d\n", time.Since(start).Round(time.Millisecond), resp.StatusCode, delay, i+1)
		}(i, delay)
		time.Sleep(*interval)
	}
	wg.Wait()
	fmt.Printf("summary sent=%d ok=%d failed=%d\n", len(delays), ok.Load(), failed.Load())
}
