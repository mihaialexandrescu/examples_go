package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
)

func LogRequestHTTP(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info(
			"received http request",
			slog.String("method", r.Method),
			slog.String("url", r.URL.Path),
		)
		next.ServeHTTP(w, r)
	})
}

func RecoverPanicHTTP(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				msg := fmt.Sprintf("recover from panic: %s", rec)
				logger.Error(msg, slog.String("method", r.Method), slog.String("url", r.URL.Path))
				w.Header().Set("Connection", "close")
				http.Error(w, msg, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
