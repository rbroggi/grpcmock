package httphandler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
	// No direct dependency on specific store implementation like inmemory here
)

// store is a composite interface embedding what handlers need from storage.Store
type store interface {
	expectationStore  // Methods needed by ExpectationHandler
	verificationStore // Methods needed by VerificationHandler
}

// StartHTTPServer starts the HTTP control plane server.
func StartHTTPServer(httpPort string, appStore store) (*http.Server, func()) { // Renamed 'store' to 'appStore'
	mux := http.NewServeMux()

	expectationHandler := NewExpectationHandler(appStore)
	verificationHandler := NewVerificationHandler(appStore)

	mux.Handle("/expectations", expectationHandler)
	mux.Handle("/expectations/", expectationHandler)
	mux.Handle("/verifications", verificationHandler)
	mux.Handle("/verifications/", verificationHandler)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("grpcmock-http: HTTP control server listening on :%s", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { // [cite: 401]
			log.Fatalf("grpcmock-http: Failed to serve HTTP: %v", err)
		}
	}()

	shutdownFunc := func() {
		log.Println("grpcmock-http: Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil { // [cite: 402]
			log.Printf("grpcmock-http: HTTP server shutdown error: %v", err)
		}
		log.Println("grpcmock-http: HTTP server gracefully stopped.")
	}

	return httpServer, shutdownFunc
}
