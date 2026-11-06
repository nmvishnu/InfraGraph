package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"infragraph/internal/graph"
)

// Server is the InfraGraph HTTP API and static file server.
type Server struct {
	client *graph.Client
	fileFS http.FileSystem // embedded or disk-based filesystem
}

// NewServer creates a new API server backed by the provided graph client.
// webFS is the filesystem to serve static files from (can be embed.FS or os directory).
func NewServer(client *graph.Client, webFS http.FileSystem) *Server {
	return &Server{client: client, fileFS: webFS}
}

// NewServerFromDir creates a server using a directory path for static files (legacy mode).
func NewServerFromDir(client *graph.Client, webRoot string) *Server {
	return &Server{client: client, fileFS: http.Dir(webRoot)}
}

// Start registers routes and begins listening on addr (e.g. ":8080").
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/query/dangling-dns", s.handleDanglingDNS)
	mux.HandleFunc("/api/query/shared-infra", s.handleSharedInfra)
	mux.HandleFunc("/api/query/tech-exposure", s.handleTechExposure)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/health", s.handleHealth) // diagnostic endpoint

	// Static frontend files (from embedded FS or disk)
	mux.Handle("/", http.FileServer(s.fileFS))

	log.Printf("[api] InfraGraph UI → http://localhost%s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      recoveryMiddleware(corsMiddleware(mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 90 * time.Second,
	}
	return srv.ListenAndServe()
}

// ──────────────────────────────────────────────────────────────────────────────
// Handlers
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	resp, err := graph.GetFullGraph(ctx, s.client, limit)
	if err != nil {
		apiError(w, "full-graph query failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleDanglingDNS(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := graph.QueryDanglingDNS(ctx, s.client)
	if err != nil {
		apiError(w, "dangling-dns query failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleSharedInfra(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := graph.QuerySharedInfrastructure(ctx, s.client)
	if err != nil {
		apiError(w, "shared-infra query failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleTechExposure(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := graph.QueryTechnologyExposure(ctx, s.client)
	if err != nil {
		apiError(w, "tech-exposure query failed", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, resp)
}

// StatsResponse holds high-level counts for the dashboard summary bar.
type StatsResponse struct {
	Domains      int `json:"domains"`
	IPs          int `json:"ips"`
	CNAMEs       int `json:"cnames"`
	Technologies int `json:"technologies"`
	Edges        int `json:"edges"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	rows, err := s.client.RunRead(ctx, `
		MATCH (n) RETURN labels(n)[0] AS label, count(n) AS cnt
	`, nil)
	if err != nil {
		apiError(w, "stats query failed", err, http.StatusInternalServerError)
		return
	}

	var stats StatsResponse
	for _, row := range rows {
		lbl, _ := row["label"].(string)
		cnt, _ := row["cnt"].(int64)
		switch lbl {
		case "Domain":
			stats.Domains = int(cnt)
		case "IP":
			stats.IPs = int(cnt)
		case "CNAME":
			stats.CNAMEs = int(cnt)
		case "Technology":
			stats.Technologies = int(cnt)
		}
	}

	edgeRows, err := s.client.RunRead(ctx, `MATCH ()-[r]->() RETURN count(r) AS cnt`, nil)
	if err == nil && len(edgeRows) > 0 {
		if cnt, ok := edgeRows[0]["cnt"].(int64); ok {
			stats.Edges = int(cnt)
		}
	}

	writeJSON(w, stats)
}

// handleHealth runs each Cypher query and reports OK / error for each.
// Hit /api/health to diagnose "Query not defined" and similar errors.
type healthResult struct {
	Database     string `json:"database"`
	Neo4j        string `json:"neo4j,omitempty"`
	FullGraph    string `json:"full_graph"`
	DanglingDNS  string `json:"dangling_dns"`
	SharedInfra  string `json:"shared_infra"`
	TechExposure string `json:"tech_exposure"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := healthResult{}

	if err := s.client.Verify(ctx); err != nil {
		res.Database = "error: " + err.Error()
		res.Neo4j = res.Database
	} else {
		res.Database = "ok"
		res.Neo4j = "ok (sqlite)"
	}

	if _, err := graph.GetFullGraph(ctx, s.client, 5); err != nil {
		res.FullGraph = "error: " + err.Error()
	} else {
		res.FullGraph = "ok"
	}

	if _, err := graph.QueryDanglingDNS(ctx, s.client); err != nil {
		res.DanglingDNS = "error: " + err.Error()
	} else {
		res.DanglingDNS = "ok"
	}

	if _, err := graph.QuerySharedInfrastructure(ctx, s.client); err != nil {
		res.SharedInfra = "error: " + err.Error()
	} else {
		res.SharedInfra = "ok"
	}

	if _, err := graph.QueryTechnologyExposure(ctx, s.client); err != nil {
		res.TechExposure = "error: " + err.Error()
	} else {
		res.TechExposure = "ok"
	}

	writeJSON(w, res)
}

// ──────────────────────────────────────────────────────────────────────────────
// Middleware & helpers
// ──────────────────────────────────────────────────────────────────────────────

// recoveryMiddleware catches Go panics and returns a 500 with the error message
// instead of silently closing the connection.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[api] PANIC recovered: %v\n%s", rec, debug.Stack())
				http.Error(w,
					fmt.Sprintf(`{"error":"internal server error","detail":"%v"}`, rec),
					http.StatusInternalServerError,
				)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "http://localhost") || origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiError writes a structured JSON error response and logs it.
func apiError(w http.ResponseWriter, msg string, err error, code int) {
	log.Printf("[api] %s: %v", msg, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  msg,
		"detail": err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] encode error: %v", err)
	}
}
