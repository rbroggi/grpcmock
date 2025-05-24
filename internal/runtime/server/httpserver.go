package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/rbroggi/grpcmock/internal/runtime"
)

// storeInterface defines the methods that a store should implement.
type storeInterface interface {
	AddExpectation(exp runtime.GRPCCallExpectation) error
	GetExpectations() map[string][]runtime.GRPCCallExpectation
	GetRecordedCalls() []runtime.RecordedGRPCCall
	ClearAll()
}

// writeErrorResponse writes an error response in JSON format.
func writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message, "details": err.Error()})
}

// writeJSONResponse writes a response in JSON format.
func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// StartHTTPServer starts the HTTP server for mock control using the provided store.
// It returns a function to gracefully shutdown the server.
func StartHTTPServer(httpPort string, httpMux *http.ServeMux, store storeInterface) (*http.Server, func()) {
	if httpMux == nil {
		httpMux = http.NewServeMux() // Create a new one if nil
	}
	httpMux.HandleFunc("/expectations", func(w http.ResponseWriter, r *http.Request) {
		handleExpectations(w, r, store)
	})
	httpMux.HandleFunc("/verifications", func(w http.ResponseWriter, r *http.Request) {
		handleVerifications(w, r, store)
	})

	// Add endpoints for match counts and satisfaction verification
	typedStore, ok := store.(interface {
		GetMatchCounts() map[string]int
		GetExpectations() map[string][]runtime.GRPCCallExpectation
	})
	if ok {
		httpMux.HandleFunc("/verifications/counts", func(w http.ResponseWriter, r *http.Request) {
			writeJSONResponse(w, http.StatusOK, typedStore.GetMatchCounts())
		})
		httpMux.HandleFunc("/verifications/satisfied", func(w http.ResponseWriter, r *http.Request) {
			result := make(map[string]bool)
			counts := typedStore.GetMatchCounts()
			expectations := typedStore.GetExpectations()
			for method, exps := range expectations {
				for idx, exp := range exps {
					key := fmt.Sprintf("%s#%d", method, idx)
					count := counts[key]
					ok := true
					if exp.Times != nil {
						if exp.Times.Exact > 0 {
							ok = count == exp.Times.Exact
						} else {
							if exp.Times.Min > 0 && count < exp.Times.Min {
								ok = false
							}
							if exp.Times.Max > 0 && count > exp.Times.Max {
								ok = false
							}
						}
					}
					result[key] = ok
				}
			}
			writeJSONResponse(w, http.StatusOK, result)
		})
	}

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

// handleExpectations manages HTTP requests for CRUD operations on expectations.
func handleExpectations(w http.ResponseWriter, r *http.Request, store storeInterface) {
	switch r.Method {
	case http.MethodPost:
		var exp runtime.GRPCCallExpectation
		if err := json.NewDecoder(r.Body).Decode(&exp); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Failed to decode expectation", err)
			return
		}
		if err := store.AddExpectation(exp); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Invalid expectation", err)
			return
		}
		writeJSONResponse(w, http.StatusCreated, map[string]string{"message": "Expectation added"})
	case http.MethodGet:
		writeJSONResponse(w, http.StatusOK, store.GetExpectations())
	case http.MethodDelete:
		store.ClearAll() // Clears both expectations and recorded calls
		writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All expectations and recorded calls cleared"})
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed", nil)
	}
}

// handleVerifications manages HTTP requests for retrieving recorded calls.
func handleVerifications(w http.ResponseWriter, r *http.Request, store storeInterface) {
	switch r.Method {
	case http.MethodGet:
		writeJSONResponse(w, http.StatusOK, store.GetRecordedCalls())
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed", nil)
	}
}
