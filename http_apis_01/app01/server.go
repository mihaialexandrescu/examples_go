package app01

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mihaialexandrescu/examples_go/http_apis_01/middleware"
)

type server struct {
	handler http.Handler
	logger  *slog.Logger
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func NewServer(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, logger)

	var handler http.Handler = mux
	handler = middleware.LogRequestHTTP(logger, handler)
	handler = middleware.RecoverPanicHTTP(logger, handler)

	srv := &server{logger: logger, handler: handler}
	return srv
}

func handleGreet1(greeting string) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			msg := fmt.Sprintf("%s %s: %s, world!\n", r.Method, r.URL, greeting)
			w.Write([]byte(msg))
		},
	)
}

func handleGreet2(greeting string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msg := fmt.Sprintf("%s %s: %s, world!\n", r.Method, r.URL, greeting)
		w.Write([]byte(msg))
	}
}

func handleReadyz(logger *slog.Logger) http.Handler {
	init := new(sync.Once)
	dummyWait := func(t time.Duration) {
		<-time.After(t)
	}
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Fake startup time
			init.Do(func() { dummyWait(4 * time.Second) })
			// Fake dependency services status collection time
			dummyWait(1 * time.Second)

			msg := map[string]string{"status": "ok"}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(msg); err != nil {
				logger.Warn("failed to encode readyz message", slog.String("msg", fmt.Sprintf("%v", msg)))
				w.WriteHeader(http.StatusInternalServerError)
			}
		})
}
