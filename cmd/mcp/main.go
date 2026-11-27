package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// stdout belongs exclusively to the MCP stdio transport.
	log.SetOutput(os.Stderr)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("CTRAIL MCP: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := mcpserver.LoadConfig()
	if err != nil {
		return err
	}
	server, err := mcpserver.NewServer(cfg)
	if err != nil {
		return err
	}
	if cfg.Transport == "stdio" {
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	handler, err := mcpserver.NewHTTPHandler(server, cfg.HTTPToken)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr: cfg.ListenAddr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: cfg.Timeout + 5*time.Second, IdleTimeout: 60 * time.Second,
	}
	failed := make(chan error, 1)
	go func() { failed <- srv.ListenAndServe() }()
	log.Printf("CTRAIL MCP listening on %s/mcp", cfg.ListenAddr)
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return err
		}
		return nil
	}
}
