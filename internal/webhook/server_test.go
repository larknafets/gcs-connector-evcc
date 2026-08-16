package webhook

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSync_ValidTokenTriggersAndReturns202(t *testing.T) {
	trigger := make(chan struct{}, 1)
	s := &Server{Secret: "s3cr3t", Trigger: trigger}
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/sync", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer s3cr3t")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	select {
	case <-trigger:
	default:
		t.Fatal("expected a trigger signal")
	}
}

func TestHandleSync_MissingOrWrongTokenIsRejected(t *testing.T) {
	trigger := make(chan struct{}, 1)
	s := &Server{Secret: "s3cr3t", Trigger: trigger}
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"wrong secret", "Bearer wrong"},
		{"wrong scheme", "Basic s3cr3t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/sync", nil)
			require.NoError(t, err)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			select {
			case <-trigger:
				t.Fatal("did not expect a trigger signal")
			default:
			}
		})
	}
}

func TestHandleSync_WrongMethodIsRejected(t *testing.T) {
	s := &Server{Secret: "s3cr3t", Trigger: make(chan struct{}, 1)}
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/sync", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer s3cr3t")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleSync_PendingTriggerIsNotBlockedByASecondRequest(t *testing.T) {
	trigger := make(chan struct{}, 1)
	s := &Server{Secret: "s3cr3t", Trigger: trigger}
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	for i := 0; i < 2; i++ {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/sync", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer s3cr3t")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	}

	assert.Len(t, trigger, 1)
}

func TestServe_ServesUntilContextCanceled(t *testing.T) {
	trigger := make(chan struct{}, 1)
	s := &Server{Secret: "s3cr3t", Trigger: trigger}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()

	req, err := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/sync", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer s3cr3t")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after context cancellation")
	}
}
