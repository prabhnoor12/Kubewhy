package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/model"
)

const maxRequestBytes int64 = 5 << 20

type Server struct {
	engine *diagnosis.Engine
	log    *log.Logger
}

func NewServer(engine *diagnosis.Engine, logger *log.Logger) *Server {
	if engine == nil {
		engine = diagnosis.NewEngine()
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Server{engine: engine, log: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/diagnose", s.diagnose)
	mux.HandleFunc("POST /api/v1/diagnose/pod", s.diagnose)
	return withJSON(withRequestID(mux))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "kubewhy"})
}

func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "request body is required")
		return
	}
	defer r.Body.Close()
	if r.ContentLength > maxRequestBytes {
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
		writeError(w, status, "invalid request: "+err.Error())
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "invalid request: multiple JSON values are not allowed")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request: trailing data")
		return
	}
	if strings.TrimSpace(request.Pod.Metadata.Name) == "" {
		writeError(w, http.StatusBadRequest, "pod.metadata.name is required")
		return
	}
	writeJSON(w, http.StatusOK, s.engine.Diagnose(request))
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
		next.ServeHTTP(w, r)
	})
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
