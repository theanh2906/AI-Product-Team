package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theanh2906/AI-Product-Team/internal/codex"
	"github.com/theanh2906/AI-Product-Team/internal/web"
)

func main() {
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}

	serviceContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var codexRuntime *codex.Client
	connectedRuntime, err := codex.Dial(serviceContext, codex.Config{})
	if err != nil {
		log.Printf("Codex app-server is unavailable; the UI will stay online: %v", err)
	} else {
		codexRuntime = connectedRuntime
		defer func() {
			if err := codexRuntime.Close(); err != nil {
				log.Printf("Codex app-server shutdown failed: %v", err)
			}
		}()
		log.Printf("Codex app-server connected over stdio")
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           web.NewServerWithCodex(codexRuntime),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("ProductCrew UI is available at http://%s", addr)
	serverError := make(chan error, 1)
	go func() { serverError <- server.ListenAndServe() }()

	select {
	case err := <-serverError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
			os.Exit(1)
		}
	case <-serviceContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			fmt.Fprintf(os.Stderr, "server shutdown failed: %v\n", err)
			os.Exit(1)
		}
	}
}
