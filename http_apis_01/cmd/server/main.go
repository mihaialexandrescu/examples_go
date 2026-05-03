package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/mihaialexandrescu/examples_go/http_apis_01/app01"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	app01_flagset := flag.NewFlagSet("app01", flag.ExitOnError)
	addr := app01_flagset.String("addr", ":4000", "HTTP network address")
	if err := app01_flagset.Parse(args); err != nil {
		return err // might want to handle differently considering the flag.ExitOnError setting above
	}
	// Example of manually defined 'args' for testing (from the article; not customized):
	// args := []string{"myapp", "--out", outFile, "--fmt", "markdown"}

	logger := slog.New(slog.NewTextHandler(stdout, nil))

	app01_srv := app01.NewServer(logger)

	httpServer := &http.Server{
		Addr:         *addr,
		Handler:      app01_srv,
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelError),
		IdleTimeout:  time.Minute,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starting http server", "addr", httpServer.Addr)
		err := httpServer.ListenAndServe()
		if err != nil {
			fmt.Fprintf(stderr, "error during ListenAndServe: %s\n", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "error shutting down http server: %s\n", err)
		}
	}()
	wg.Wait()

	return nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(100)
	}
}
