// Package apiserver owns the HTTP boundary. Huma and net/http types stop here.
package apiserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Config struct {
	AllowedOrigins                                                             []string
	ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout, ShutdownTimeout time.Duration
	MaxBodyBytes                                                               int64
	MaxHeaderBytes                                                             int
	// TrustedProxies may supply X-Forwarded-For; RateLimitPerMinute and RateLimitBurst bound
	// requests per client IP (IPv6 per /64).
	TrustedProxies                     []netip.Prefix
	RateLimitPerMinute, RateLimitBurst uint64
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func DefaultConfig() Config {
	return Config{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, ShutdownTimeout: 5 * time.Second, MaxBodyBytes: 6 << 20, MaxHeaderBytes: 32 << 10, RateLimitPerMinute: 600, RateLimitBurst: 120}
}

type Readiness interface{ Ready(context.Context) error }
type ReadyFunc func(context.Context) error

func (f ReadyFunc) Ready(ctx context.Context) error { return f(ctx) }

type Server struct {
	Handler http.Handler
	HTTP    *http.Server
	API     huma.API

	streamShutdown chan struct{}
	shutdownOnce   sync.Once
}

type HealthResponse struct {
	Status string `json:"status"`
}
type humaHealthOutput struct {
	Status int `status:"200" json:"-"`
	Body   HealthResponse
}

func New(cfg Config, ready Readiness, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	defaults := DefaultConfig()
	if cfg.ReadHeaderTimeout <= 0 {
		cfg.ReadHeaderTimeout = defaults.ReadHeaderTimeout
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaults.ReadTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaults.WriteTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaults.IdleTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaults.ShutdownTimeout
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if cfg.MaxHeaderBytes <= 0 {
		cfg.MaxHeaderBytes = defaults.MaxHeaderBytes
	}
	if cfg.RateLimitPerMinute == 0 || cfg.RateLimitBurst == 0 {
		cfg.RateLimitPerMinute, cfg.RateLimitBurst = defaults.RateLimitPerMinute, defaults.RateLimitBurst
	}
	mux := http.NewServeMux()
	api := humago.NewWithPrefix(mux, "/v1", huma.DefaultConfig("Launchpad API", "1.0.0"))
	huma.Register(api, huma.Operation{OperationID: "healthz", Method: http.MethodGet, Path: "/healthz", Tags: []string{"system"}}, func(context.Context, *struct{}) (*humaHealthOutput, error) {
		return &humaHealthOutput{Status: http.StatusOK, Body: HealthResponse{Status: "ok"}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "readyz", Method: http.MethodGet, Path: "/readyz", Tags: []string{"system"}}, func(ctx context.Context, _ *struct{}) (*humaHealthOutput, error) {
		if ready == nil {
			return nil, huma.Error503ServiceUnavailable("readiness unavailable")
		}
		if err := ready.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("service not ready")
		}
		return &humaHealthOutput{Status: http.StatusOK, Body: HealthResponse{Status: "ready"}}, nil
	})
	// Keep orchestration endpoints available at their conventional root paths;
	// the versioned aliases are included in the generated contract.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready == nil || ready.Ready(r.Context()) != nil {
			problem(w, http.StatusServiceUnavailable, "service not ready", RequestID(r.Context()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ready"})
	})
	h := middleware(mux, cfg, NewRateLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst), logger)
	return &Server{
		Handler:        h,
		API:            api,
		HTTP:           &http.Server{Handler: h, ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: cfg.MaxHeaderBytes},
		streamShutdown: make(chan struct{}),
	}
}

func (s *Server) RegisterTokenRoutes(r TokenRoutes)       { r.Register(s.API) }
func (s *Server) RegisterQuoteRoutes(r QuoteRoutes)       { r.Register(s.API) }
func (s *Server) RegisterCandleRoutes(r CandleRoutes)     { r.Register(s.API) }
func (s *Server) RegisterPublicRoutes(r PublicRoutes)     { r.Register(s.API) }
func (s *Server) RegisterMetadataRoutes(r MetadataRoutes) { r.Register(s.API) }
func (s *Server) RegisterEventRoutes(r EventRoutes) {
	r.shutdown = s.streamShutdown
	r.Register(s.API)
}
func (s *Server) RegisterObservationRoutes(r ObservationRoutes) { r.Register(s.API) }
func (s *Server) RegisterProfileRoutes(r ProfileRoutes)         { r.Register(s.API) }

func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() { close(s.streamShutdown) })
	return s.HTTP.Shutdown(ctx)
}

type ctxKey string

const requestIDKey ctxKey = "request_id"
const authHeadersKey ctxKey = "auth_headers"

type AuthHeaders struct {
	Authorization string
	IdentityToken string
}

func RequestID(ctx context.Context) string { v, _ := ctx.Value(requestIDKey).(string); return v }
func ExtractedAuth(ctx context.Context) AuthHeaders {
	v, _ := ctx.Value(authHeadersKey).(AuthHeaders)
	return v
}

func middleware(next http.Handler, cfg Config, limiter *RateLimiter, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		status := http.StatusOK
		rw := &statusWriter{ResponseWriter: w, status: &status}
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !requestIDPattern.MatchString(id) {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err == nil {
				id = fmt.Sprintf("%x", raw)
			} else {
				id = fmt.Sprintf("%d", time.Now().UnixNano())
			}
		}
		rw.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		clientKey := clientKeyFor(clientAddress(r, cfg.TrustedProxies))
		ctx = context.WithValue(ctx, clientKeyContextKey, clientKey)
		defer func() {
			if v := recover(); v != nil {
				problem(rw, http.StatusInternalServerError, "internal server error", id)
				logger.Error("panic recovered", "request_id", id)
			}
			logger.Info("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds())
		}()
		if r.Method == http.MethodOptions {
			if !cors(rw, cfg.AllowedOrigins, r) {
				problem(rw, http.StatusForbidden, "origin not allowed", id)
				return
			}
			rw.WriteHeader(http.StatusNoContent)
			return
		}
		cors(rw, cfg.AllowedOrigins, r)
		if !probePath(r.URL.Path) {
			if allowed, wait := limiter.Allow(clientKey, time.Now()); !allowed {
				rw.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Round(time.Second)/time.Second))))
				problem(rw, http.StatusTooManyRequests, "rate limit exceeded", id)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(rw, r.Body, cfg.MaxBodyBytes)
		}
		if r.URL.Path != "/v1/events" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, cfg.ReadTimeout)
			defer cancel()
		} else {
			// net/http's server-wide WriteTimeout is an absolute request deadline.
			// Disable it for this long-lived stream; heartbeat writes still detect
			// dead clients and shutdown cancels the request context.
			_ = http.NewResponseController(rw).SetWriteDeadline(time.Time{})
		}
		ctx = context.WithValue(ctx, authHeadersKey, AuthHeaders{Authorization: r.Header.Get("Authorization"), IdentityToken: r.Header.Get("privy-id-token")})
		r = r.WithContext(ctx)
		next.ServeHTTP(rw, r)
	})
}

func probePath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/v1/healthz", "/v1/readyz":
		return true
	default:
		return false
	}
}

type statusWriter struct {
	http.ResponseWriter
	status *int
}

func (w *statusWriter) WriteHeader(code int) { *w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func cors(w http.ResponseWriter, origins []string, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range origins {
		if allowed == origin && allowed != "*" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,privy-id-token,If-Match,If-None-Match")
			w.Header().Set("Access-Control-Expose-Headers", "ETag, X-Revision")
			return true
		}
	}
	return false
}
func problem(w http.ResponseWriter, status int, detail, requestID string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "request_id": requestID})
}
