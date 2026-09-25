package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"

	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/model"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const maxRequestBytes int64 = 5 << 20

var (
	metricsOnce sync.Once

	diagnosisDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubewhy_diagnosis_duration_seconds",
			Help:    "Time taken to complete a diagnosis",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status", "confidence"},
	)
	diagnosisRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubewhy_diagnosis_requests_total",
			Help: "Total number of diagnosis requests",
		},
		[]string{"status"},
	)
	collectionErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kubewhy_collection_errors_total",
			Help: "Total number of collection errors",
		},
	)
)

func registerMetrics() {
	metricsOnce.Do(func() {
		prometheus.MustRegister(diagnosisDuration, diagnosisRequests, collectionErrorsCounter)
	})
}

type ipLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
}

func newIPLimiter(r rate.Limit, burst int) *ipLimiter {
	l := &ipLimiter{limiters: make(map[string]*rate.Limiter), rate: r, burst: burst}
	go l.cleanup()
	return l
}

func (l *ipLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.RLock()
	limiter, exists := l.limiters[ip]
	l.mu.RUnlock()
	if exists {
		return limiter
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter = rate.NewLimiter(l.rate, l.burst)
	l.limiters[ip] = limiter
	return limiter
}

func (l *ipLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip := range l.limiters {
			delete(l.limiters, ip)
		}
		l.mu.Unlock()
	}
}

func withRateLimit(limiter *ipLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !limiter.getLimiter(ip).Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type Server struct {
	engine *diagnosis.Engine
	logger *slog.Logger

	diagnosisDuration  *prometheus.HistogramVec
	diagnosisRequests  *prometheus.CounterVec
	collectionErrors   prometheus.Counter
	rateLimiter        *ipLimiter
}

func NewServer(engine *diagnosis.Engine, logger *slog.Logger) *Server {
	if engine == nil {
		engine = diagnosis.NewEngine()
	}
	if logger == nil {
		logger = slog.Default()
	}

	registerMetrics()

	rateLimiter := newIPLimiter(rate.Limit(10), 20)

	return &Server{
		engine:             engine,
		logger:             logger,
		diagnosisDuration:  diagnosisDuration,
		diagnosisRequests:  diagnosisRequests,
		collectionErrors:   collectionErrorsCounter,
		rateLimiter:        rateLimiter,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/diagnose", s.diagnose)
	mux.HandleFunc("POST /api/v1/diagnose/pod", s.diagnose)
	mux.Handle("GET /metrics", promhttp.Handler())
	return withRateLimit(s.rateLimiter)(withJSON(withRequestID(mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "kubewhy"})
}

func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	logger := s.logger.With(slog.String("request_id", requestIDFromContext(r.Context())))
	if r.Body == nil {
		logger.Warn("request rejected: missing body", slog.String("status", "400"))
		writeError(w, http.StatusBadRequest, "request body is required")
		return
	}
	defer r.Body.Close()
	if r.ContentLength > maxRequestBytes {
		logger.Warn("request rejected: body too large", slog.Int64("content_length", r.ContentLength))
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds 5 MiB limit")
		return
	}
	var request model.DiagnoseRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err := decoder.Decode(&request); err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		logger.Warn("request rejected: invalid JSON", slog.Int("status", status), slog.String("error", err.Error()))
		writeError(w, status, "invalid request: "+err.Error())
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			logger.Warn("request rejected: multiple JSON values")
			writeError(w, http.StatusBadRequest, "invalid request: multiple JSON values are not allowed")
			return
		}
		logger.Warn("request rejected: trailing data")
		writeError(w, http.StatusBadRequest, "invalid request: trailing data")
		return
	}
	if strings.TrimSpace(request.Pod.Metadata.Name) == "" {
		logger.Warn("request rejected: missing pod name")
		writeError(w, http.StatusBadRequest, "pod.metadata.name is required")
		return
	}
	start := time.Now()
	report := s.engine.Diagnose(request)
	duration := time.Since(start)

	s.diagnosisDuration.WithLabelValues(report.Status, report.Confidence).Observe(duration.Seconds())
	s.diagnosisRequests.WithLabelValues(report.Status).Inc()
	if len(request.CollectionErrors) > 0 {
		s.collectionErrors.Add(float64(len(request.CollectionErrors)))
	}

	logger.Info("diagnosis completed",
		slog.String("pod", request.Pod.Metadata.Name),
		slog.String("namespace", request.Pod.Metadata.Namespace),
		slog.String("status", report.Status),
		slog.String("confidence", report.Confidence),
		slog.Int("reasons", len(report.Reasons)),
		slog.Duration("duration", duration),
	)
	writeJSON(w, http.StatusOK, report)
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 || strings.ContainsAny(requestID, "\r\n") {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return fmt.Sprintf("%x", bytes)
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
