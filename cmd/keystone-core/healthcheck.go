package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const healthURL = "http://127.0.0.1" + listenAddr + "/api/healthz"

// healthcheck backs the container HEALTHCHECK: the distroless image has no
// shell or curl, so the binary probes itself.
func healthcheck(ctx context.Context) error { return runHealthcheck(ctx, healthURL) }

func runHealthcheck(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: status %d", resp.StatusCode)
	}
	return nil
}
