package app

import (
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/netx"
)

func TestNetworkServiceDefaultsToDirectAndPersistsUpdates(t *testing.T) {
	directory := t.TempDir()
	store, err := database.Open(t.Context(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service, err := NewNetworkService(t.Context(), store.Client)
	if err != nil {
		t.Fatal(err)
	}
	config, err := service.Network(t.Context())
	if err != nil || config != (netx.ProxyConfig{}) || service.ProxyManager().Resolve() != nil {
		t.Fatalf("initial network config = %+v, err=%v", config, err)
	}

	want := netx.ProxyConfig{Enabled: true, URL: "http://127.0.0.1:7890"}
	if err := service.UpdateNetwork(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	if got, _ := service.Network(t.Context()); got != want {
		t.Fatalf("updated config = %+v, want %+v", got, want)
	}

	restarted, err := NewNetworkService(t.Context(), store.Client)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := restarted.Network(t.Context()); got != want || restarted.ProxyManager().Resolve() == nil {
		t.Fatalf("persisted config = %+v", got)
	}
}

func TestNetworkServiceRejectsInvalidUpdateWithoutChangingCurrentValue(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := NewNetworkService(t.Context(), store.Client)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateNetwork(t.Context(), netx.ProxyConfig{Enabled: true, URL: "ftp://127.0.0.1:21"}); err == nil {
		t.Fatal("invalid proxy URL was accepted")
	}
	if got, _ := service.Network(t.Context()); got != (netx.ProxyConfig{}) {
		t.Fatalf("invalid update changed config: %+v", got)
	}
}
