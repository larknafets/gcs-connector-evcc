package cli

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupWebhookListener_DisabledWhenPortUnset(t *testing.T) {
	trigger, start, err := setupWebhookListener(config.Config{}, nil)
	require.NoError(t, err)
	assert.Nil(t, trigger)
	assert.Nil(t, start)
}

func TestSetupWebhookListener_ReturnsErrorOnPortConflict(t *testing.T) {
	// Matches the ":<port>" (all-interfaces) form setupWebhookListener binds
	// itself - occupying only "127.0.0.1:<port>" isn't guaranteed to
	// conflict with a wildcard bind on every OS.
	occupied, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer occupied.Close()

	port := occupied.Addr().(*net.TCPAddr).Port
	cfg := config.Config{Webhook: config.WebhookConfig{Port: port, Secret: "s3cr3t"}}

	_, _, err = setupWebhookListener(cfg, nil)
	require.Error(t, err)
}

func TestSetupWebhookListener_BindsAndAcceptsConnections(t *testing.T) {
	// Grab an ephemeral port, then release it immediately so
	// setupWebhookListener can bind it itself.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())

	cfg := config.Config{Webhook: config.WebhookConfig{Port: port, Secret: "s3cr3t"}}

	trigger, start, err := setupWebhookListener(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, start)
	require.NotNil(t, trigger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		start(ctx)
		close(done)
	}()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	require.Eventually(t, func() bool {
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr != nil {
			return false
		}
		conn.Close()
		return true
	}, time.Second, 10*time.Millisecond, "webhook listener never came up on the bound port")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("start did not return after context cancellation")
	}
}
