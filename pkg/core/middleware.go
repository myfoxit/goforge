package core

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/myfoxit/goforge/pkg/security"
)

// Middleware is a standard http middleware.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares left-to-right (first wraps outermost).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// WithRecover converts panics into 500 responses.
func (a *App) WithRecover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					a.log.Error("panic recovered", "err", rec, "path", r.URL.Path)
					WriteJSON(w, http.StatusInternalServerError, &APIError{
						Status: 500, Message: "Something went wrong while processing your request.",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type requestIDKey struct{}

// WithRequestID assigns a request id and echoes it as X-Request-Id.
func (a *App) WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				id = security.RandomID(12)
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		})
	}
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE streaming working through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// RequestLogFunc receives finished requests (logs module, metrics, ...).
type RequestLogFunc func(r *http.Request, status int, dur time.Duration, auth *Auth)

var requestLogFns []RequestLogFunc
var requestLogMu sync.RWMutex

// AddRequestLogFunc registers a request sink (multiple allowed).
func AddRequestLogFunc(fn RequestLogFunc) {
	requestLogMu.Lock()
	requestLogFns = append(requestLogFns, fn)
	requestLogMu.Unlock()
}

// SetRequestLogFunc is a legacy alias for AddRequestLogFunc.
func SetRequestLogFunc(fn RequestLogFunc) { AddRequestLogFunc(fn) }

// WithLogger logs requests to slog (debug) and the optional db sink.
func (a *App) WithLogger() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			dur := time.Since(start)
			a.log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", dur.String())
			requestLogMu.RLock()
			fns := requestLogFns
			requestLogMu.RUnlock()
			for _, fn := range fns {
				fn(r, rec.status, dur, AuthFromContext(r.Context()))
			}
		})
	}
}

// WithCORS handles cross-origin requests for browser clients.
func (a *App) WithCORS() Middleware {
	origins := a.cfg.HTTP.CORSOrigins
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := ""
				for _, o := range origins {
					if o == "*" || strings.EqualFold(o, origin) {
						allowed = origin
						break
					}
				}
				if allowed != "" {
					h := w.Header()
					h.Set("Access-Control-Allow-Origin", allowed)
					h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, MCP-Protocol-Version, Last-Event-ID")
					h.Set("Access-Control-Max-Age", "86400")
					h.Set("Vary", "Origin")
				}
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithAuth resolves credentials into an Auth on the context. The default
// resolver handles Bearer JWTs; modules may add more (API keys, ...).
func (a *App) WithAuth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := a.resolveAuth(r)
			if auth != nil {
				r = r.WithContext(WithAuthContext(r.Context(), auth))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *App) resolveAuth(r *http.Request) *Auth {
	header := r.Header.Get("Authorization")
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if raw == header {
		raw = strings.TrimSpace(header) // bare token without scheme
	}
	if raw != "" && strings.Count(raw, ".") == 2 { // JWT shape
		if auth, err := a.VerifyAuthToken(r.Context(), raw); err == nil {
			return auth
		}
	}
	for _, resolver := range a.authResolvers {
		if auth, err := resolver(r); err == nil && auth != nil {
			return auth
		}
	}
	return nil
}

// RequireAuth wraps a handler, rejecting unauthenticated requests.
func (a *App) RequireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if AuthFromContext(r.Context()) == nil {
			WriteError(w, a.log, Unauthorized(""))
			return
		}
		h(w, r)
	}
}

// RequireSuperuser wraps a handler, allowing only superusers.
func (a *App) RequireSuperuser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := AuthFromContext(r.Context())
		if auth == nil {
			WriteError(w, a.log, Unauthorized(""))
			return
		}
		if !auth.IsSuperuser() {
			WriteError(w, a.log, Forbidden("Superuser access required."))
			return
		}
		h(w, r)
	}
}

// RequireRole wraps a handler, allowing superusers or users with any of the
// given role names.
func (a *App) RequireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			auth := AuthFromContext(r.Context())
			if auth == nil {
				WriteError(w, a.log, Unauthorized(""))
				return
			}
			if auth.IsSuperuser() {
				h(w, r)
				return
			}
			for _, want := range roles {
				for _, have := range auth.Roles {
					if want == have {
						h(w, r)
						return
					}
				}
			}
			WriteError(w, a.log, Forbidden(""))
		}
	}
}

// ---- rate limiting ----

type rateBucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	rate    float64
	burst   float64
}

func newRateLimiter(rate float64, burst int) *rateLimiter {
	rl := &rateLimiter{buckets: map[string]*rateBucket{}, rate: rate, burst: float64(burst)}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if time.Since(b.last) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &rateBucket{tokens: rl.burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimit returns a middleware limiting requests per client IP.
// Used with tight values on auth endpoints to slow brute force.
func (a *App) RateLimit(perSecond float64, burst int) func(http.HandlerFunc) http.HandlerFunc {
	rl := newRateLimiter(perSecond, burst)
	return func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(a.ClientIP(r)) {
				WriteError(w, a.log, TooManyRequests())
				return
			}
			h(w, r)
		}
	}
}

// ClientIP extracts the caller IP, honoring X-Forwarded-For only when
// the app is configured to trust a fronting proxy.
func (a *App) ClientIP(r *http.Request) string {
	if a.cfg.HTTP.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
		if rip := r.Header.Get("X-Real-Ip"); rip != "" {
			return rip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
