// keystone-gate is the forward-auth service for apps behind a reverse proxy.
//
//	keystone-gate [serve]      listen on :8081
//	keystone-gate healthcheck  exit 0 if the local server answers /healthz
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/civr/keystone/internal/gate"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
)

const listenAddr = ":8081"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	var err error
	switch cmd {
	case "serve":
		err = serve(ctx)
	case "healthcheck":
		err = healthcheck(ctx)
	default:
		fmt.Fprintf(os.Stderr, "usage: keystone-gate [serve|healthcheck]\n")
		os.Exit(2)
	}
	if err != nil {
		slog.Error("keystone-gate: "+cmd, "err", err)
		os.Exit(1)
	}
}

func healthcheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1"+listenAddr+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: status %d", resp.StatusCode)
	}
	return nil
}

func serve(ctx context.Context) error {
	sec, pool, err := runtime.OpenDB(ctx, false)
	if err != nil {
		return err
	}
	defer pool.Close()
	rdb, err := runtime.Valkey(ctx, sec)
	if err != nil {
		return err
	}
	defer rdb.Close()
	g := &gate.Server{
		View:     &runtime.Snapshot{Pool: pool, TTL: 5 * time.Second},
		Valkey:   rdb,
		Sessions: session.NewStore(rdb),
		Limiter:  session.NewLimiter(rdb),
	}
	srv := &http.Server{
		Addr: listenAddr, Handler: g.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("keystone-gate listening", "addr", listenAddr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
