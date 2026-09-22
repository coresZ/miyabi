package javbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/time/rate"
)

// JavBus has a single public endpoint; there are no mirrors to manage.
const (
	baseURL        = "https://www.javbus.com"
	defaultTimeout = 15 * time.Second
	defaultRate    = 1
	defaultBurst   = 2
	detailCacheTTL = 5 * time.Minute
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
	CloseIdleConnections()
}

// Options configures a JavBus client.
type Options struct {
	Timeout time.Duration
	Proxy   *netx.ProxyManager

	testClient httpClient
}

// Client accesses JavBus for movie magnets and metadata.
type Client struct {
	timeout      time.Duration
	proxyManager *netx.ProxyManager
	proxyChanges <-chan struct{}
	limiter      *rate.Limiter
	cache        *detailCache

	clientMu sync.RWMutex
	client   httpClient

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a JavBus client.
func New(options Options) (*Client, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &Client{
		timeout:      timeout,
		proxyManager: options.Proxy,
		limiter:      rate.NewLimiter(rate.Every(time.Second/time.Duration(defaultRate)), defaultBurst),
		cache:        newDetailCache(detailCacheTTL),
		ctx:          ctx,
		cancel:       cancel,
	}

	if options.testClient != nil {
		client.client = options.testClient
		return client, nil
	}

	initialClient, err := client.buildHTTPClient(client.resolveProxy())
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create JavBus fingerprint client: %w", err)
	}
	client.client = initialClient

	if options.Proxy != nil {
		client.proxyChanges = options.Proxy.Subscribe()
		go client.watchProxy()
	}

	return client, nil
}

// Name identifies the magnet source.
func (c *Client) Name() string {
	return domain.MagnetSourceJavBus
}

// Find retrieves magnets for the specified movie reference.
func (c *Client) Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error) {
	code := strings.TrimSpace(ref.Code)
	if code == "" {
		return nil, nil
	}

	// Skip categories JavBus does not curate or formats with irregular quality.
	if ref.Zone == domain.ZoneWestern || ref.Zone == domain.ZoneAnime || ref.Zone == domain.ZoneFC2 {
		return nil, nil
	}
	if strings.HasPrefix(strings.ToUpper(code), "FC2") {
		return nil, nil
	}

	gid, uc, img, err := c.ensureDetailParams(ctx, code)
	if err != nil {
		return nil, err
	}
	if gid == "" {
		// Movie was not found on JavBus.
		return nil, nil
	}

	return c.fetchMagnets(ctx, code, gid, uc, img)
}

func (c *Client) ensureDetailParams(ctx context.Context, code string) (gid, uc, img string, err error) {
	if entry, ok := c.cache.get(code); ok {
		return entry.gid, entry.uc, entry.img, nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return "", "", "", err
	}

	detailURL := fmt.Sprintf("%s/%s?existmag=all", baseURL, url.PathEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", "dv=1; existmag=all")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := c.getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", "", "", ctx.Err()
		}
		return "", "", "", domain.E(domain.KindUpstream, "JavBus detail request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 301 || resp.StatusCode == 302 {
		location := resp.Header.Get("Location")
		if strings.Contains(location, "driver-verify") {
			return "", "", "", domain.E(domain.KindUpstream, "JavBus driver verify required", nil)
		}
		return "", "", "", domain.E(domain.KindUpstream, fmt.Sprintf("JavBus unexpected redirect to %s", location), nil)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", "", domain.E(domain.KindUpstream, "read JavBus detail response", err)
	}
	bodyStr := string(body)

	if resp.StatusCode == 404 || isNotFoundPage(bodyStr) {
		return "", "", "", nil
	}

	if isCloudflareChallenge(bodyStr) || resp.StatusCode == 403 || resp.StatusCode == 503 {
		return "", "", "", domain.E(domain.KindUpstream, "JavBus Cloudflare challenge encountered", nil)
	}
	if isDriverVerify(bodyStr) {
		return "", "", "", domain.E(domain.KindUpstream, "JavBus driver verify required", nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", domain.E(domain.KindUpstream, fmt.Sprintf("JavBus returned unexpected status %d", resp.StatusCode), nil)
	}

	params, err := extractDetailParams(bodyStr)
	if err != nil {
		return "", "", "", err
	}

	c.cache.set(code, bodyStr, params.GID, params.UC, params.Img)
	return params.GID, params.UC, params.Img, nil
}

func (c *Client) fetchMagnets(ctx context.Context, code, gid, uc, img string) ([]domain.Magnet, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	floor := rand.IntN(1000) + 1
	ajaxURL := fmt.Sprintf("%s/ajax/uncledatoolsbyajax.php?gid=%s&lang=zh&img=%s&uc=%s&floor=%d",
		baseURL, url.QueryEscape(gid), url.QueryEscape(img), url.QueryEscape(uc), floor)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ajaxURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", "dv=1; existmag=all")
	req.Header.Set("Referer", fmt.Sprintf("%s/%s", baseURL, url.PathEscape(code)))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	client := c.getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, domain.E(domain.KindUpstream, "JavBus ajax magnets request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 301 || resp.StatusCode == 302 {
		return nil, domain.E(domain.KindUpstream, "JavBus driver verify required", nil)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, domain.E(domain.KindUpstream, "read JavBus ajax magnets response", err)
	}
	bodyStr := string(body)

	if isCloudflareChallenge(bodyStr) || resp.StatusCode == 403 || resp.StatusCode == 503 {
		return nil, domain.E(domain.KindUpstream, "JavBus Cloudflare challenge encountered", nil)
	}
	if isDriverVerify(bodyStr) {
		return nil, domain.E(domain.KindUpstream, "JavBus driver verify required", nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, domain.E(domain.KindUpstream, fmt.Sprintf("JavBus returned unexpected status %d", resp.StatusCode), nil)
	}

	return parseMagnetsHTML(bodyStr)
}

func (c *Client) getHTTPClient() httpClient {
	c.clientMu.RLock()
	defer c.clientMu.RUnlock()
	return c.client
}

func (c *Client) resolveProxy() *url.URL {
	if c.proxyManager == nil {
		return nil
	}
	return c.proxyManager.Resolve()
}

func (c *Client) buildHTTPClient(proxy *url.URL) (httpClient, error) {
	return netx.NewFingerprintClient(netx.FingerprintOptions{
		Timeout:   c.timeout,
		Proxy:     proxy,
		CookieJar: true,
	})
}

func (c *Client) watchProxy() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case _, ok := <-c.proxyChanges:
			if !ok {
				return
			}
			newClient, err := c.buildHTTPClient(c.resolveProxy())
			if err != nil {
				slog.WarnContext(c.ctx, "JavBus transport keeps previous proxy after change", "error", err)
				continue
			}
			c.clientMu.Lock()
			oldClient := c.client
			c.client = newClient
			c.clientMu.Unlock()
			if oldClient != nil {
				oldClient.CloseIdleConnections()
			}
		}
	}
}

// Close releases network resources and proxy subscription.
func (c *Client) Close() {
	c.cancel()
	if c.proxyManager != nil && c.proxyChanges != nil {
		c.proxyManager.Unsubscribe(c.proxyChanges)
	}
	c.clientMu.Lock()
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	c.clientMu.Unlock()
}
