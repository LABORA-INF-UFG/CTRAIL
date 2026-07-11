package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/config"
	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/mep"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules"
	"github.com/gin-gonic/gin"
)

// enableEAM builds the Kubernetes (and optional OAI MEP) backend clients and
// activates the Edge Application Management instance on the orchestrator.
func enableEAM(orchestrator *modules.GatewayOrchestrator, cfg *config.Config) error {
	k8sClient, err := k8s.NewClient(cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	var mepClient mep.Client
	if cfg.MEPEnabled {
		mepClient = mep.NewClient(cfg.MEPBaseURL)
		log.Printf("OAI MEP service registration enabled (%s)", cfg.MEPBaseURL)
	}

	orchestrator.EnableEAM(k8sClient, cfg.K8sNamespacePrefix, mepClient)
	return nil
}

func main() {
	log.Printf("Starting CAMARA to NEF Gateway with modular architecture")

	cfg := config.Load()
	log.Printf("Configuration loaded - NEF URL: %s, Server Port: %s", cfg.NEFBaseURL, cfg.ServerPort)

	orchestrator := modules.NewGatewayOrchestrator(cfg.NEFBaseURL, cfg.NEFToken)

	if cfg.EAMEnabled {
		if err := enableEAM(orchestrator, cfg); err != nil {
			log.Printf("Edge Application Management disabled: %v", err)
		}
	}

	r := gin.Default()

	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s %s\n",
			param.TimeStamp.Format("2006-01-02 15:04:05"),
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
		)
	}))

	r.Use(gin.Recovery())

	orchestrator.SetupSecureRoutes(r)

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting server on port %s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server startup failed: %v", err)
		}
	}()

	if cfg.PprofPort != "" {
		go func() {
			log.Printf("Starting pprof server on port %s (http://localhost:%s/debug/pprof/)", cfg.PprofPort, cfg.PprofPort)
			if err := http.ListenAndServe(":"+cfg.PprofPort, nil); err != nil {
				log.Printf("pprof server error: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := orchestrator.Shutdown(ctx); err != nil {
		log.Printf("Gateway orchestrator shutdown error: %v", err)
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Printf("Server shutdown completed successfully")
}
