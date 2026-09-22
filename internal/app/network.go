package app

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/javbus"
	"github.com/ppxb/miyabi/internal/javdb"
	"github.com/ppxb/miyabi/internal/netx"
)

const (
	networkProxySetting = "network.proxy"
	networkProbeTimeout = 8 * time.Second
)

// NetworkService owns the persisted upstream network configuration.
type NetworkService struct {
	database *ent.Client
	proxy    *netx.ProxyManager
}

func NewNetworkService(ctx context.Context, database *ent.Client) (*NetworkService, error) {
	config, _, err := loadSetting[netx.ProxyConfig](ctx, database, networkProxySetting)
	if err != nil {
		return nil, err
	}
	proxy, err := netx.NewProxyManager(config)
	if err != nil {
		return nil, fmt.Errorf("load network proxy setting: %w", err)
	}
	return &NetworkService{database: database, proxy: proxy}, nil
}

func (service *NetworkService) ProxyManager() *netx.ProxyManager {
	return service.proxy
}

func (service *NetworkService) Network(context.Context) (netx.ProxyConfig, error) {
	return service.proxy.Config(), nil
}

// UpdateNetwork validates, persists and broadcasts the configuration.
func (service *NetworkService) UpdateNetwork(ctx context.Context, config netx.ProxyConfig) error {
	normalized, _, err := netx.Normalize(config)
	if err != nil {
		return err
	}
	if err := saveSetting(ctx, service.database, networkProxySetting, normalized); err != nil {
		return err
	}
	return service.proxy.Update(normalized)
}

// TestNetwork probes JavDB and JavBus concurrently through the candidate configuration without persisting it.
func (service *NetworkService) TestNetwork(ctx context.Context, config netx.ProxyConfig) (netx.NetworkTestResponse, error) {
	_, proxy, err := netx.Normalize(config)
	if err != nil {
		return netx.NetworkTestResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, networkProbeTimeout)
	defer cancel()

	var response netx.NetworkTestResponse
	var wait sync.WaitGroup
	probe := func(target *netx.NetworkProbeResult, measure func(context.Context, *url.URL, time.Duration) (time.Duration, error)) {
		defer wait.Done()
		latency, err := measure(ctx, proxy, networkProbeTimeout)
		if err != nil {
			*target = netx.NetworkProbeResult{Error: err.Error()}
			return
		}
		*target = netx.NetworkProbeResult{Available: true, LatencyMS: latency.Milliseconds()}
	}
	wait.Add(2)
	go probe(&response.JavDB, javdb.Probe)
	go probe(&response.JavBus, javbus.Probe)
	wait.Wait()
	return response, nil
}
