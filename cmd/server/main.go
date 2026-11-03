// Command server runs the LINE Research Planner Bot on Cloud Run.
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

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/api/idtoken"
	"google.golang.org/genai"

	"github.com/kkdai/line-research-bot/internal/config"
	"github.com/kkdai/line-research-bot/internal/linebot"
	"github.com/kkdai/line-research-bot/internal/publisher"
	"github.com/kkdai/line-research-bot/internal/research"
	"github.com/kkdai/line-research-bot/internal/server"
	"github.com/kkdai/line-research-bot/internal/state"
	"github.com/kkdai/line-research-bot/internal/tasks"
	"github.com/kkdai/line-research-bot/internal/worker"
)

func main() {
	// Cloud Logging-friendly JSON logs.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.LevelKey:
				a.Key = "severity"
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	})))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.UseFirestoreEmulator {
		os.Setenv("FIRESTORE_EMULATOR_HOST", cfg.FirestoreEmulatorHost)
	}
	fs, err := firestore.NewClient(ctx, cfg.GCPProjectID)
	if err != nil {
		return fmt.Errorf("firestore: %w", err)
	}
	defer fs.Close()
	store := state.New(fs)

	gcs, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer gcs.Close()

	line, err := linebot.New(cfg.LineChannelAccessToken, cfg.LineChannelSecret)
	if err != nil {
		return fmt.Errorf("line: %w", err)
	}

	llm, err := gemini.NewModel(ctx, cfg.GeminiModel, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  cfg.GCPProjectID,
		Location: cfg.GCPLocation,
	})
	if err != nil {
		return fmt.Errorf("gemini model: %w", err)
	}
	agents, err := research.New(llm, cfg.AgentID)
	if err != nil {
		return err
	}

	targetURL := "http://placeholder/tasks/run-research"
	var verify server.TokenVerifier
	if cfg.CloudRunServiceURL != "" {
		targetURL = cfg.CloudRunServiceURL + "/tasks/run-research"
		verify = oidcVerifier(targetURL, cfg.ServiceAccountEmail)
	} else {
		slog.Warn("CLOUD_RUN_SERVICE_URL is not set; Cloud Tasks target is a placeholder and OIDC verification is disabled")
	}
	dispatcher, err := tasks.New(ctx, cfg.GCPProjectID, cfg.CloudTasksLocation, cfg.CloudTasksQueue,
		targetURL, targetURL, cfg.ServiceAccountEmail)
	if err != nil {
		return fmt.Errorf("cloud tasks: %w", err)
	}
	defer dispatcher.Close()

	srv := &server.Server{
		Store:       store,
		Line:        line,
		Tasks:       dispatcher,
		Worker:      worker.New(store, agents, line, publisher.NewGCS(gcs, cfg.GCSBucket), cfg.GCSBucket),
		VerifyToken: verify,
	}
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "port", cfg.Port, "model", cfg.GeminiModel, "agent", cfg.AgentID)
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(sctx)
	}
	return nil
}

// oidcVerifier validates Google-signed OIDC tokens minted by Cloud Tasks for
// the given audience and service account.
func oidcVerifier(audience, saEmail string) server.TokenVerifier {
	return func(ctx context.Context, bearer string) error {
		p, err := idtoken.Validate(ctx, bearer, audience)
		if err != nil {
			return err
		}
		if email, _ := p.Claims["email"].(string); email != saEmail {
			return fmt.Errorf("unexpected token email %q", email)
		}
		return nil
	}
}
