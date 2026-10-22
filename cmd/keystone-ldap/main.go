// keystone-ldap is the LDAP server for apps that only speak LDAP.
//
//	keystone-ldap [serve]      LDAPS on :636, plain LDAP on :389 (closed while ldap.require_tls is on)
//	keystone-ldap healthcheck  exit 0 if the LDAPS port accepts connections
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/keys"
	"github.com/civr/keystone/internal/ldapsrv"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

const (
	ldapsAddr = ":636"
	ldapAddr  = ":389"
)

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
		var c net.Conn
		if c, err = net.DialTimeout("tcp", "127.0.0.1"+ldapsAddr, 3*time.Second); err == nil {
			c.Close()
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: keystone-ldap [serve|healthcheck]\n")
		os.Exit(2)
	}
	if err != nil {
		slog.Error("keystone-ldap: "+cmd, "err", err)
		os.Exit(1)
	}
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
	aw := audit.NewWriter(pool, settings.NewStore(pool))
	defer aw.Close()

	// The LDAPS certificate is the "ldap" signing key; rotating it in
	// Instellingen takes effect for the next connection.
	ks := keys.New(pool, sec.DataKey)
	tc := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			k, err := ks.Active(context.Background(), keys.LDAP)
			if err != nil {
				return nil, err
			}
			return &tls.Certificate{Certificate: [][]byte{k.Cert.Raw}, PrivateKey: k.Signer, Leaf: k.Cert}, nil
		},
	}
	if _, err := ks.Active(ctx, keys.LDAP); err != nil {
		return err
	}
	srv := &ldapsrv.Server{
		View:    &runtime.Snapshot{Pool: pool, TTL: 5 * time.Second},
		Dir:     runtime.Directory(pool, sec.DataKey),
		Limiter: session.NewLimiter(rdb),
		Audit:   aw,
	}
	ln, err := net.Listen("tcp", ldapsAddr)
	if err != nil {
		return err
	}
	plain, err := net.Listen("tcp", ldapAddr)
	if err != nil {
		return err
	}
	slog.Info("keystone-ldap listening", "ldaps", ldapsAddr, "ldap", ldapAddr)
	errc := make(chan error, 2)
	go func() { errc <- srv.Serve(ctx, tls.NewListener(ln, tc), false) }()
	go func() { errc <- srv.Serve(ctx, plain, true) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return nil
	}
}
