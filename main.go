package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"device-registry/internal/handler"
	"device-registry/internal/repository"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*addr, logger); err != nil {
		logger.Error("HTTP server stopped", "error", err)
		os.Exit(1)
	}
}

func run(addr string, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler.NewRouter(repository.NewMemoryDeviceRepository(), logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
	logger.Info("Device Registry API listening", "address", listener.Addr(), "route", "/api/v1/devices")
	select {
	case err := <-ended:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
