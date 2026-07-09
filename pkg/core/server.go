package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Serve bootstraps (if needed) and runs the HTTP server until ctx is
// cancelled or SIGINT/SIGTERM arrives, then shuts down gracefully.
func (a *App) Serve(ctx context.Context) error {
	if !a.bootstrapped {
		if err := a.Bootstrap(ctx); err != nil {
			return err
		}
	}
	if err := a.OnServe.Trigger(&ServeEvent{App: a, Mux: a.mux}); err != nil {
		return err
	}

	handler := Chain(a.mux,
		a.WithRecover(),
		a.WithRequestID(),
		a.WithCORS(),
		a.WithAuth(),
		a.WithLogger(),
	)

	srv := &http.Server{
		Addr:              a.cfg.HTTP.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 2)

	if domains := a.cfg.HTTP.HTTPSDomains; len(domains) > 0 {
		manager := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(domains...),
			Cache:      autocert.DirCache(filepath.Join(a.cfg.DataDir, ".autocert")),
		}
		srv.Addr = ":443"
		srv.TLSConfig = &tls.Config{
			GetCertificate: manager.GetCertificate,
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"h2", "http/1.1", "acme-tls/1"},
		}
		// Port 80: ACME HTTP-01 challenges + redirect to https.
		redirect := &http.Server{
			Addr: ":80",
			Handler: manager.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := "https://" + r.Host + r.URL.RequestURI()
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			})),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
		go func() {
			a.log.Info("server started", "addr", srv.Addr, "https", true, "domains", domains)
			if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
		defer redirect.Shutdown(context.Background())
	} else {
		go func() {
			a.log.Info("server started", "url", fmt.Sprintf("http://localhost%s", displayAddr(srv.Addr)),
				"admin", fmt.Sprintf("http://localhost%s/_/", displayAddr(srv.Addr)))
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	a.log.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		a.log.Warn("shutdown incomplete", "err", err)
	}
	a.OnTerminate.Trigger(&TerminateEvent{App: a})
	if a.db != nil {
		a.db.Close()
	}
	return nil
}

// Start is the convenience entrypoint: bootstrap + serve with background ctx.
func (a *App) Start() error {
	return a.Serve(context.Background())
}

func displayAddr(addr string) string {
	if addr == "" {
		return ":80"
	}
	return addr
}
