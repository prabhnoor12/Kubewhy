package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/model"
)

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
	var request model.DiagnoseRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20))
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if request.Pod.Metadata.Name == "" {
		writeError(w, http.StatusBadRequest, "pod.metadata.name is required")
		return
	}
	writeJSON(w, http.StatusOK, s.engine.Diagnose(request))
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("X-Request-ID") == "" {
			w.Header().Set("X-Request-ID", time.Now().UTC().Format("20060102T150405.000000000Z"))
		}
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
