package handler

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"device-registry/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(repo repository.DeviceRepository, logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, logRequests(logger), recoverPanics(logger), cors)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusNotFound, "Route does not exist")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusNotImplemented, "Server does not implement this HTTP method")
	})
	h := deviceHandler{devices: repo}
	r.Route(devicesPath, func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		resourceMethods(r, "GET, POST, OPTIONS")
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Put("/", h.update)
			r.Delete("/", h.remove)
			resourceMethods(r, "GET, PUT, DELETE, OPTIONS")
		})
	})
	return r
}

func resourceMethods(r chi.Router, allow string) {
	r.Options("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		w.WriteHeader(http.StatusNoContent)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		problem(w, http.StatusMethodNotAllowed, "This method is unavailable for the route")
	})
}

func logRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				logger.Info("HTTP request", "request_id", middleware.GetReqID(r.Context()),
					"method", r.Method, "path", r.URL.RequestURI(), "client", r.RemoteAddr,
					"status", wrapped.Status(), "duration", time.Since(start).String())
			}()
			next.ServeHTTP(wrapped, r)
		})
	}
}

func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				if err := recover(); err != nil {
					if err == http.ErrAbortHandler {
						panic(err)
					}
					logger.Error("Handler panic", "request_id", middleware.GetReqID(r.Context()), "panic", err, "stack", string(debug.Stack()))
					if wrapped.Status() != 0 {
						panic(http.ErrAbortHandler)
					}
					problem(wrapped, http.StatusInternalServerError, "Unexpected server error")
				}
			}()
			next.ServeHTTP(wrapped, r)
		})
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Location")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		next.ServeHTTP(w, r)
	})
}
