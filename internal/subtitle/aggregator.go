package subtitle

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/ppxb/miyabi/internal/netx"
)

type Aggregator struct {
	providers     []Provider
	client        *resty.Client
	allowLoopback bool
}

type AggregatorOption func(*Aggregator)

// WithAllowLoopbackForTesting allows loopback IP addresses for safe downloads, strictly for unit tests.
func WithAllowLoopbackForTesting(allow bool) AggregatorOption {
	return func(a *Aggregator) {
		a.allowLoopback = allow
		if allow {
			a.client = netx.NewDirectRestyClient(netx.RestyOptions{Timeout: 10 * time.Second})
		}
	}
}

func NewAggregator(proxyManager *netx.ProxyManager, opts ...AggregatorOption) *Aggregator {
	a := &Aggregator{
		providers: []Provider{
			NewXunleiProvider(proxyManager),
			NewSubtitleCatProvider(proxyManager),
		},
		client: netx.NewSafeDownloadClient(proxyManager, 20*time.Second),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Search queries all registered providers concurrently, ranks the results, and returns deduplicated candidates.
func (a *Aggregator) Search(ctx context.Context, code string, isUncensoredVideo bool) ([]Candidate, error) {
	if code == "" {
		return nil, nil
	}

	var mu sync.Mutex
	var allCandidates []Candidate
	var wg sync.WaitGroup

	for _, p := range a.providers {
		wg.Add(1)
		go func(prov Provider) {
			defer wg.Done()
			results, err := prov.Search(ctx, code)
			if err == nil && len(results) > 0 {
				mu.Lock()
				allCandidates = append(allCandidates, results...)
				mu.Unlock()
			}
		}(p)
	}

	wg.Wait()

	if len(allCandidates) == 0 {
		return nil, nil
	}

	// Deduplicate by URL
	seenURL := make(map[string]bool)
	unique := make([]Candidate, 0, len(allCandidates))
	for _, c := range allCandidates {
		if c.URL != "" && !seenURL[c.URL] {
			seenURL[c.URL] = true
			unique = append(unique, c)
		}
	}

	ranked := RankCandidates(unique, code, isUncensoredVideo)
	return ranked, nil
}

// DownloadAndConvert downloads the subtitle from candidate URL, decodes character encoding to UTF-8,
// and standardizes it into WebVTT format.
func (a *Aggregator) DownloadAndConvert(ctx context.Context, c Candidate) (string, error) {
	if c.URL == "" {
		return "", fmt.Errorf("empty candidate URL")
	}

	var opts []netx.DownloadOption
	if a.allowLoopback {
		opts = append(opts, netx.WithAllowLoopback(true))
	}
	rawBytes, err := netx.SafeDownload(ctx, a.client, c.URL, opts...)
	if err != nil {
		return "", fmt.Errorf("download subtitle: %w", err)
	}

	utf8Text, err := DecodeToUTF8(rawBytes)
	if err != nil {
		return "", fmt.Errorf("decode subtitle encoding: %w", err)
	}

	vtt, err := ConvertToWebVTT(utf8Text, c.Ext)
	if err != nil {
		return "", fmt.Errorf("convert to webvtt: %w", err)
	}

	return vtt, nil
}
