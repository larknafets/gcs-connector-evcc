// Package webhook implements the connector's optional HTTP listener: a
// single POST endpoint that lets an external trigger - typically evcc's
// messaging "stop" event, fired when a charging session ends - request an
// immediate sync cycle instead of waiting for the next scheduled tick. It
// never talks to evcc or GCS itself; it only forwards a non-blocking signal
// to loop.Runner's Trigger channel.
package webhook

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// shutdownTimeout bounds how long Serve waits for an in-flight request to
// finish once ctx is canceled.
const shutdownTimeout = 5 * time.Second

// Server exposes POST /sync, authenticated with a bearer token, that
// forwards a trigger signal for an immediate sync cycle.
type Server struct {
	// Secret is the expected bearer token. A request without a matching
	// "Authorization: Bearer <Secret>" header is rejected with 401.
	Secret string
	// Trigger receives a non-blocking signal per authorized request. It is
	// typically the buffered channel also assigned to loop.Runner.Trigger.
	Trigger chan<- struct{}
	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Handler returns the server's HTTP handler, exposed separately from Serve
// so it can be exercised directly (e.g. via httptest.NewServer) in tests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sync", s.handleSync)
	return mux
}

// Serve accepts connections on ln until ctx is canceled, then shuts down
// gracefully (an in-flight request is allowed to finish, mirroring the
// daemon loop's own shutdown behavior).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.Handler()}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		s.logger().Warn("webhook: rejected request with invalid or missing bearer token", "remote_addr", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	select {
	case s.Trigger <- struct{}{}:
	default:
		// A trigger is already pending; the cycle it causes will still
		// cover this request's charging session, so dropping it here is
		// correct, not lossy.
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.Secret)) == 1
}
