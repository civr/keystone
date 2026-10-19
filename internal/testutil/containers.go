// Package testutil starts throwaway Postgres and Valkey containers for tests.
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Postgres starts a postgres:17-alpine container and returns its DSN.
// It skips the test under -short.
func Postgres(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping container test in -short mode")
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("keystone"),
		postgres.WithUsername("keystone"),
		postgres.WithPassword("keystone"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	return dsn
}

// Valkey starts a valkey/valkey:8-alpine container and returns a redis:// URL.
// It skips the test under -short.
func Valkey(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping container test in -short mode")
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start valkey: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("valkey host: %v", err)
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("valkey port: %v", err)
	}
	return "redis://" + host + ":" + port.Port()
}
