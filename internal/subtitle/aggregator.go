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
	providers []Provider
	client    *resty.Client
}

func NewAggregator(proxyManager *netx.ProxyManager) *Aggregator {
	var client *resty.Client
	opts := netx.RestyOptions{Timeout: 20 * time.Second}
	if proxyManager != nil {
		client = netx.NewRestyClient(proxyManager, opts)
	} else {
		client = netx.NewDirectRestyClient(opts)
	}
	return &Aggregator{
		providers: []Provider{
			NewXunleiProvider(proxyManager),
			NewSubtitleCatProvider(proxyManager),
		},
		client: client,
	}
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

	resp, err := a.client.R().SetContext(ctx).Get(c.URL)
	if err != nil {
		return "", fmt.Errorf("download subtitle: %w", err)
	}
	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("download subtitle status %d", resp.StatusCode())
	}

	rawBytes := resp.Body()
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
