package httphandler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/rbroggi/grpcmock/internal/runtime/acl"
	"github.com/rbroggi/grpcmock/internal/runtime/api"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"github.com/rbroggi/grpcmock/internal/runtime/storage"
)

type verificationStore interface {
	GetExpectation(ctx context.Context, id string) (*core.Expectation, error)
	ListCalls(ctx context.Context, fullMethodNameFilter string) ([]*core.RecordedCall, error)
	ClearAllCalls(ctx context.Context) error
	GetMatchCount(ctx context.Context, expectationID string) (int, error)
}

// VerificationHandler handles HTTP requests for call verifications and expectation status.
type VerificationHandler struct {
	store verificationStore
	// matcher matching.Service // May not be needed here, expectationStore has counts
}

// NewVerificationHandler creates a new VerificationHandler.
func NewVerificationHandler(s verificationStore) *VerificationHandler {
	return &VerificationHandler{store: s}
}

// ServeHTTP routes requests.
// Base path: /verifications
func (h *VerificationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	trimmedPath := strings.TrimPrefix(r.URL.Path, "/verifications")
	trimmedPath = strings.Trim(trimmedPath, "/")

	// GET /verifications -> List all recorded calls
	// DELETE /verifications -> Clear all recorded calls
	// GET /verifications/expectations/{expID} -> Get status (match count, satisfied) of a specific expectation

	switch r.Method {
	case http.MethodGet:
		if trimmedPath == "" {
			h.handleListRecordedCalls(w, r)
		} else if strings.HasPrefix(trimmedPath, "expectations/") {
			expID := strings.TrimPrefix(trimmedPath, "expectations/")
			h.handleGetExpectationStatus(w, r, expID)
		} else {
			writeErrorResponse(w, http.StatusNotFound, "Path not found")
		}
	case http.MethodDelete:
		if trimmedPath == "" {
			h.handleClearAllRecordedCalls(w, r)
		} else {
			writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed for this path")
		}

	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *VerificationHandler) handleListRecordedCalls(w http.ResponseWriter, r *http.Request) {
	fullMethodNameFilter := r.URL.Query().Get("fullMethodName")

	coreCalls, err := h.store.ListCalls(r.Context(), fullMethodNameFilter)
	if err != nil {
		log.Printf("httphandler: Error listing recorded calls: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to list recorded calls", err.Error())
		return
	}

	apiCalls := make([]*api.RecordedGRPCCall, 0, len(coreCalls))
	for _, coreCall := range coreCalls {
		apiCall, err := acl.ToAPIRecordedCall(coreCall)
		if err != nil {
			log.Printf("httphandler: Error translating core recorded call to API DTO (ID: %s): %v", coreCall.ID, err)
			continue // Skip this problematic call
		}
		apiCalls = append(apiCalls, apiCall)
	}
	writeJSONResponse(w, http.StatusOK, apiCalls)
}

func (h *VerificationHandler) handleClearAllRecordedCalls(w http.ResponseWriter, r *http.Request) {
	if err := h.store.ClearAllCalls(r.Context()); err != nil {
		log.Printf("httphandler: Error clearing all recorded calls: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to clear all recorded calls", err.Error())
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All recorded calls cleared successfully"})
}

func (h *VerificationHandler) handleGetExpectationStatus(w http.ResponseWriter, r *http.Request, expectationID string) {
	if expectationID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "Expectation ID is required")
		return
	}

	expectation, err := h.store.GetExpectation(r.Context(), expectationID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeErrorResponse(w, http.StatusNotFound, "Expectation not found", err.Error())
		} else {
			log.Printf("httphandler: Error getting expectation %s: %v", expectationID, err)
			writeErrorResponse(w, http.StatusInternalServerError, "Failed to get expectation", err.Error())
		}
		return
	}

	matchCount, err := h.store.GetMatchCount(r.Context(), expectationID)
	if err != nil {
		log.Printf("httphandler: Error getting match count for expectation %s: %v", expectationID, err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get match count", err.Error())
		return
	}

	isSatisfied := true
	if expectation.ExpectedCallTimes != nil {
		isSatisfied = expectation.ExpectedCallTimes.IsSatisfied(matchCount)
	}

	statusResponse := map[string]interface{}{
		"expectationId": expectationID,
		"matchCount":    matchCount,
		"isSatisfied":   isSatisfied,
		"expectedCalls": expectation.ExpectedCallTimes, // Can be nil
	}

	writeJSONResponse(w, http.StatusOK, statusResponse)
}
