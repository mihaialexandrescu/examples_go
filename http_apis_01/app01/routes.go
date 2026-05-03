package app01

import (
	"log/slog"
	"net/http"
)

func addRoutes(mux *http.ServeMux, logger *slog.Logger) {
	_ = logger
	mux.Handle("GET /greet1/{$}", handleGreet1("Hello"))
	mux.HandleFunc("GET /greet2/{$}", handleGreet2("Hello"))
	mux.Handle("GET /readyz", handleReadyz(logger))
	mux.Handle("/", http.NotFoundHandler())
}
