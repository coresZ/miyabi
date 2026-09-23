package pan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/time/rate"
)

func TestPanTransport_RateLimitsRetries(t *testing.T) {
	var requestTimes []time.Time
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestTimes = append(requestTimes, time.Now())
		count := len(requestTimes)
		mu.Unlock()

		if count < 3 {
			// Fail first 2 attempts with 502 to trigger Resty retry
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":1,"code":0,"data":{"ok":true}}`))
	}))
	defer ts.Close()

	gap := 100 * time.Millisecond
	limiter := rate.NewLimiter(rate.Every(gap), 1)

	restyClient := resty.New()
	transport := newPanTransport(http.DefaultTransport, limiter, 2)
	restyClient.SetTransport(transport)
	restyClient.SetRetryCount(3)
	restyClient.SetRetryWaitTime(10 * time.Millisecond) // Short retry wait to test limiter constraint
	restyClient.AddRetryCondition(func(r *resty.Response, err error) bool {
		return r != nil && r.StatusCode() == http.StatusBadGateway
	})

	client := &Client{
		http:    restyClient,
		limiter: limiter,
	}

	req := client.http.R().SetContext(context.Background())
	_, err := client.request(req, http.MethodGet, ts.URL)
	if err != nil {
		t.Fatalf("expected request success after retries, got: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestTimes) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(requestTimes))
	}

	for i := 1; i < len(requestTimes); i++ {
		diff := requestTimes[i].Sub(requestTimes[i-1])
		// Even though resty retry wait time was 10ms, panTransport limiter must enforce >= 90ms
		if diff < 90*time.Millisecond {
			t.Errorf("attempt %d -> %d interval %v was shorter than limiter gap %v", i-1, i, diff, gap)
		}
	}
}

func TestPanTransport_LimitsInFlightConcurrency(t *testing.T) {
	var currentInFlight int64
	var maxObservedInFlight int64

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&currentInFlight, 1)
		for {
			max := atomic.LoadInt64(&maxObservedInFlight)
			if cur <= max || atomic.CompareAndSwapInt64(&maxObservedInFlight, max, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&currentInFlight, -1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":1,"code":0,"data":{}}`))
	}))
	defer ts.Close()

	// High rate limit so rate limiter won't bottleneck in-flight test
	limiter := rate.NewLimiter(rate.Inf, 1)
	restyClient := resty.New()
	transport := newPanTransport(http.DefaultTransport, limiter, 2)
	restyClient.SetTransport(transport)

	client := &Client{
		http:    restyClient,
		limiter: limiter,
	}

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := client.http.R().SetContext(context.Background())
			_, _ = client.request(req, http.MethodGet, ts.URL)
		}()
	}
	wg.Wait()

	max := atomic.LoadInt64(&maxObservedInFlight)
	if max > 2 {
		t.Errorf("max in-flight was %d, expected <= 2", max)
	}
}

func TestPanTransport_RespectsRetryAfter(t *testing.T) {
	var attempts int64
	start := time.Now()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt64(&attempts, 1)
		if att == 1 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"state":1,"code":0,"data":{}}`))
	}))
	defer ts.Close()

	limiter := rate.NewLimiter(rate.Inf, 1)
	restyClient := resty.New()
	transport := newPanTransport(http.DefaultTransport, limiter, 2)
	restyClient.SetTransport(transport)
	restyClient.SetRetryCount(2)
	restyClient.SetRetryMaxWaitTime(10 * time.Second)
	restyClient.SetRetryAfter(func(c *resty.Client, resp *resty.Response) (time.Duration, error) {
		if resp == nil {
			return 0, nil
		}
		return parseRetryAfter(resp.Header().Get("Retry-After")), nil
	})
	restyClient.AddRetryCondition(func(r *resty.Response, err error) bool {
		return r != nil && r.StatusCode() == http.StatusTooManyRequests
	})

	client := &Client{
		http:    restyClient,
		limiter: limiter,
	}

	req := client.http.R().SetContext(context.Background())
	_, err := client.request(req, http.MethodGet, ts.URL)
	if err != nil {
		t.Fatalf("expected request success, got: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 950*time.Millisecond {
		t.Errorf("retry happened too early (%v), did not respect Retry-After: 1s", elapsed)
	}
}
