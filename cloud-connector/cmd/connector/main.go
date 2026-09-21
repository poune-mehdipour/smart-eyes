// Command connector is the Guardian Cloud Connector: it ingests telemetry
// and hazard events from Guardian edge devices and simulated third-party
// providers, normalizes and persists them in PostgreSQL, and serves an
// internal gRPC API to downstream services.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/commands"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/config"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/events"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/grpcapi"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/ingest"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/oauth"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/observability"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/provider/alphasense"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/storage"
	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/webhook"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(log)
	metrics := observability.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ---- Storage (retry at startup: in compose/K8s the database may come
	// up after us; crashing into a restart loop is noisier than waiting).
	var store *storage.Store
	for {
		store, err = storage.Open(ctx, cfg.DatabaseURL)
		if err == nil {
			break
		}
		log.Warn("database_not_ready", "error", err.Error())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	defer store.Close()
	log.Info("database_ready")

	if cfg.EdgeAPIToken == "" {
		log.Warn("edge_auth_disabled",
			"detail", "EDGE_API_TOKEN is empty; the edge ingest API accepts unauthenticated requests (development only)")
	}

	// ---- Services
	eventSvc := events.New(store, log, metrics)

	senders := map[domain.ProviderName]provider.CommandSender{}
	var pollers []*provider.Poller

	if cfg.AlphaSense.BaseURL != "" {
		tokens := oauth.NewManager(oauth.Config{
			TokenURL:     cfg.AlphaSense.TokenURL,
			ClientID:     cfg.AlphaSense.ClientID,
			ClientSecret: cfg.AlphaSense.ClientSecret,
		}, nil)
		client := alphasense.New(cfg.AlphaSense.BaseURL, tokens, nil)
		senders[domain.ProviderAlphaSense] = client
		pollers = append(pollers, &provider.Poller{
			Provider: client,
			Devices:  store,
			Events:   eventSvc,
			Log:      log,
			Metrics:  metrics,
			Interval: cfg.AlphaSense.PollInterval,
		})
		log.Info("provider_enabled", "provider", "alphasense", "mode", "oauth2+rest-polling")
	}
	if cfg.BetaGridWebhookSecret != "" {
		log.Info("provider_enabled", "provider", "betagrid", "mode", "signed-webhooks")
	}

	commandSvc := commands.New(store, log, metrics, cfg.CommandTTL, senders)
	webhookHandler := webhook.New(store, eventSvc, log, metrics,
		cfg.BetaGridWebhookSecret, cfg.WebhookMaxSkew, cfg.MaxBodyBytes)
	edgeAPI := ingest.New(store, eventSvc, commandSvc, log, metrics, cfg.EdgeAPIToken, cfg.MaxBodyBytes)

	// ---- HTTP server
	mux := http.NewServeMux()
	wrap := func(h http.Handler, route string) http.Handler {
		return observability.Middleware(h, route, log, metrics, cfg.RequestTimeout)
	}
	edgeAPI.Register(mux, wrap)
	mux.Handle("POST /v1/webhooks/betagrid", wrap(http.HandlerFunc(webhookHandler.BetaGrid), "webhook_betagrid"))
	mux.Handle("GET /healthz", observability.HealthHandler())
	mux.Handle("GET /readyz", observability.ReadyHandler(store.Ping))
	mux.Handle("GET /metrics", metrics.Handler())

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ---- gRPC server
	grpcSrv := grpc.NewServer()
	grpcapi.New(store, eventSvc, commandSvc, log).Register(grpcSrv)

	// ---- Run everything, shut down together.
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("http_listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			errCh <- err
			return
		}
		log.Info("grpc_listening", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	for _, p := range pollers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Run(ctx)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		commandSvc.RunReaper(ctx, time.Minute)
	}()

	// ---- Wait for a signal or a server failure.
	select {
	case <-ctx.Done():
		log.Info("shutting_down", "reason", "signal")
	case err := <-errCh:
		log.Error("server_failed", "error", err.Error())
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	grpcSrv.GracefulStop()
	wg.Wait()
	log.Info("stopped")
	return nil
}
