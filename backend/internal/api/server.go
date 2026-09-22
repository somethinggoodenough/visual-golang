package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"goviz/backend/internal/compiler"
	"goviz/backend/internal/engine"
)

const maxRequestBytes = 4 << 20

type Server struct{ Compiler *compiler.Compiler }

type request struct {
	RequestID       string          `json:"requestId"`
	ProgramRevision int64           `json:"programRevision"`
	Source          string          `json:"source"`
	IR              *engine.Program `json:"ir"`
	Project         *Project        `json:"project"`
}
type conversionResponse struct {
	RequestID       string `json:"requestId"`
	ProgramRevision int64  `json:"programRevision"`
	engine.Result
}
type buildResponse struct {
	RequestID       string `json:"requestId"`
	ProgramRevision int64  `json:"programRevision"`
	compiler.Result
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		version, available := s.Compiler.Version(r.Context())
		write(w, http.StatusOK, map[string]any{"requestId": r.URL.Query().Get("requestId"), "status": "ok", "goAvailable": available, "goVersion": version, "limits": map[string]int{"maxNodes": engine.MaxNodes, "maxCapacity": engine.MaxCapacity}})
	})
	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !read(w, r, &req) {
			return
		}
		if len(req.Source) > engine.MaxSourceBytes {
			apiError(w, 413, req.RequestID, "source_size", "源码不能超过 1 MiB。")
			return
		}
		result := engine.Import(req.Source)
		write(w, 200, conversionResponse{req.RequestID, req.ProgramRevision, result})
	})
	mux.HandleFunc("POST /api/generate", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !read(w, r, &req) {
			return
		}
		if !checkIRVersion(w, req) {
			return
		}
		write(w, 200, conversionResponse{req.RequestID, req.ProgramRevision, engine.Generate(req.IR)})
	})
	mux.HandleFunc("POST /api/build", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !read(w, r, &req) {
			return
		}
		if !checkIRVersion(w, req) {
			return
		}
		write(w, 200, buildResponse{req.RequestID, req.ProgramRevision, s.Compiler.Build(r.Context(), req.IR)})
	})
	mux.HandleFunc("POST /api/project/validate", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !read(w, r, &req) {
			return
		}
		if req.Project == nil {
			apiError(w, 400, req.RequestID, "project_required", "缺少项目对象。")
			return
		}
		if req.Project.FileFormatVersion != "1.0" {
			apiError(w, 422, req.RequestID, "project_version", "不支持的项目版本。")
			return
		}
		diagnostics := validateProject(req.Project)
		if len(diagnostics) > 0 {
			write(w, 200, map[string]any{"requestId": req.RequestID, "status": "invalid_project", "diagnostics": diagnostics})
			return
		}
		write(w, 200, map[string]any{"requestId": req.RequestID, "status": "ok", "project": req.Project, "diagnostics": []engine.Diagnostic{}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { apiError(w, 404, "", "not_found", "API 不存在。") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		// The local service accepts only loopback hosts/origins; this also prevents
		// unrelated websites and DNS rebinding from issuing build requests.
		if !isLoopbackHost(r.Host) {
			apiError(w, 403, "", "host_forbidden", "服务仅允许本地访问。")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !isLoopbackHost(u.Host) {
				apiError(w, 403, "", "origin_forbidden", "请求来源不受支持。")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func checkIRVersion(w http.ResponseWriter, req request) bool {
	if req.IR == nil {
		apiError(w, 400, req.RequestID, "ir_required", "缺少 IR 对象。")
		return false
	}
	if req.IR.SchemaVersion != "1.0" {
		apiError(w, 422, req.RequestID, "schema_version", "不支持的 IR 版本。")
		return false
	}
	return true
}
func read(w http.ResponseWriter, r *http.Request, req *request) bool {
	t, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || t != "application/json" {
		apiError(w, 415, "", "content_type", "请求必须使用 application/json。")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		decodeError(w, "", err)
		return false
	}
	// Read the envelope independently so nested union errors cannot prevent
	// requestId echo merely because object keys happened to arrive in that order.
	var envelope struct {
		RequestID string `json:"requestId"`
	}
	_ = json.Unmarshal(body, &envelope)
	req.RequestID = envelope.RequestID
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(req); err != nil {
		decodeError(w, req.RequestID, err)
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("JSON 后有额外数据")
		}
		decodeError(w, req.RequestID, err)
		return false
	}
	if req.RequestID == "" || len(req.RequestID) > 200 {
		apiError(w, 400, req.RequestID, "request_id", "必须提供长度为 1–200 的 requestId。")
		return false
	}
	if req.ProgramRevision < 0 || req.ProgramRevision > 9007199254740991 {
		apiError(w, 400, req.RequestID, "revision", "修订号必须是非负安全整数。")
		return false
	}
	return true
}
func decodeError(w http.ResponseWriter, id string, err error) {
	var versionErr *engine.VersionError
	if errors.As(err, &versionErr) {
		apiError(w, 422, id, "schema_version", err.Error())
		return
	}
	var sizeErr *http.MaxBytesError
	if errors.As(err, &sizeErr) {
		apiError(w, 413, id, "request_size", "请求体超过 4 MiB。")
		return
	}
	apiError(w, 400, id, "invalid_json", err.Error())
}
func apiError(w http.ResponseWriter, status int, id, code, message string) {
	write(w, status, map[string]any{"requestId": id, "status": "error", "diagnostics": []engine.Diagnostic{{Code: code, Severity: "error", Phase: "ir", Message: message}}})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
