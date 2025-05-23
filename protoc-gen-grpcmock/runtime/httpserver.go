package runtime

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
)

// StartHTTPServer starts the HTTP server for mock control.
// It returns a function to gracefully shutdown the server.
func StartHTTPServer(httpPort string, httpMux *http.ServeMux) (*http.Server, func()) {
	if httpMux == nil {
		httpMux = http.NewServeMux() // Create a new one if nil
	}
	httpMux.HandleFunc("/expectations", HandleExpectations)
	httpMux.HandleFunc("/verifications", HandleVerifications)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", httpPort),
		Handler: httpMux,
	}

	go func() {
		log.Printf("grpcmockruntime: HTTP mock control server listening on :%s", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("grpcmockruntime: failed to serve HTTP: %v", err)
		}
	}()

	shutdownFunc := func() {
		log.Println("grpcmockruntime: Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("grpcmockruntime: HTTP server shutdown error: %v", err)
		}
		log.Println("grpcmockruntime: HTTP server gracefully stopped.")
	}

	return httpServer, shutdownFunc
}

// ListenForShutdownSignal is a helper to wait for OS signals for graceful shutdown
func ListenForShutdownSignal(shutdownFuncs ...func()) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("grpcmockruntime: Shutdown signal received...")
	for _, sf := range shutdownFuncs {
		sf()
	}
}
