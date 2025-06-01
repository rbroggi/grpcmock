package httphandler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

type store interface {
	expectationStore
	verificationStore
}

// StartHTTPServer starts the HTTP control plane server.
// It returns the http.Server instance and a shutdown function.
func StartHTTPServer(
	httpPort string,
	store store, /*, matcher matching.Service*/
) (*http.Server, func()) {
	mux := http.NewServeMux()

	expectationHandler := NewExpectationHandler(store)
	verificationHandler := NewVerificationHandler(store) // Pass matcher if needed

	mux.Handle("/expectations", expectationHandler)    // Matches /expectations
	mux.Handle("/expectations/", expectationHandler)   // Matches /expectations/*
	mux.Handle("/verifications", verificationHandler)  // Matches /verifications
	mux.Handle("/verifications/", verificationHandler) // Matches /verifications/*

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("grpcmock-http: HTTP control server listening on :%s", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("grpcmock-http: Failed to serve HTTP: %v", err)
		}
	}()

	shutdownFunc := func() {
		log.Println("grpcmock-http: Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("grpcmock-http: HTTP server shutdown error: %v", err)
		}
		log.Println("grpcmock-http: HTTP server gracefully stopped.")
	}

	return httpServer, shutdownFunc
}
