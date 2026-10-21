package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"netpulse/internal/config"
	"netpulse/internal/database"
)

func Context() (context.Context, context.CancelFunc) {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", os.Args[0]))
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
func Setup(ctx context.Context) (config.Config, *database.Store, error) {
	c, err := config.Load()
	if err != nil {
		return c, nil, err
	}
	var s *database.Store
	for i := 0; i < 10; i++ {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		s, err = database.Open(attempt, c)
		cancel()
		if err == nil {
			return c, s, nil
		}
		select {
		case <-ctx.Done():
			return c, nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return c, nil, err
}
func Serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		case <-done:
		}
	}()
	err := srv.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func Fatal(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
