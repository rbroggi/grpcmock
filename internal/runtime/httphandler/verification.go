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

// verificationStore defines the subset of storage.Store methods needed by this handler.
type verificationStore interface {
	GetExpectation(ctx context.Context, id string) (*core.Expectation, error)
	ListCalls(ctx context.Context, fullMethodNameFilter string) ([]*core.RecordedCall, error)
	ClearAllCalls(ctx context.Context) error
	GetMatchCount(ctx context.Context, expectationID string) (int, error)
}

// VerificationHandler handles HTTP requests for call verifications and expectation status.
type VerificationHandler struct { // Renamed from Verification to avoid conflict
	store verificationStore
}

// NewVerificationHandler creates a new VerificationHandler.
func NewVerificationHandler(s verificationStore) *VerificationHandler { // Renamed
	return &VerificationHandler{store: s}
}

// ServeHTTP routes requests for /verifications.
func (h *VerificationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { //
	trimmedPath := strings.TrimPrefix(r.URL.Path, "/verifications")
	trimmedPath = strings.Trim(trimmedPath, "/")

	switch r.Method {
	case http.MethodGet:
		if trimmedPath == "" { // GET /verifications
			h.handleListRecordedCalls(w, r)
		} else if strings.HasPrefix(trimmedPath, "expectations/") { // GET /verifications/expectations/{expID}
			expID := strings.TrimPrefix(trimmedPath, "expectations/")
			h.handleGetExpectationStatus(w, r, expID)
		} else {
			writeErrorResponse(w, http.StatusNotFound, "Path not found")
		}
	case http.MethodDelete:
		if trimmedPath == "" { // DELETE /verifications
			h.handleClearAllRecordedCalls(w, r)
		} else {
			writeErrorResponse(w, http.StatusMethodNotAllowed, "DELETE allowed only for /verifications path")
		}
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *VerificationHandler) handleListRecordedCalls(w http.ResponseWriter, r *http.Request) {
	fullMethodNameFilter := r.URL.Query().Get("fullMethodName")

	coreCalls, err := h.store.ListCalls(r.Context(), fullMethodNameFilter)
	if err != nil { //
		log.Printf("httphandler: Error listing recorded calls: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to list recorded calls", err.Error())
		return
	}

	apiCalls := make([]*api.RecordedGRPCCall, 0, len(coreCalls)) //
	for _, coreCall := range coreCalls {
		apiCall, errConv := acl.ToAPIRecordedCall(coreCall) //
		if errConv != nil {
			log.Printf("httphandler: Error translating core recorded call to API DTO (ID: %s): %v", coreCall.ID, errConv)
			continue
		}
		apiCalls = append(apiCalls, apiCall)
	}
	writeJSONResponse(w, http.StatusOK, apiCalls)
}

func (h *VerificationHandler) handleClearAllRecordedCalls(w http.ResponseWriter, r *http.Request) {
	if err := h.store.ClearAllCalls(r.Context()); err != nil { //
		log.Printf("httphandler: Error clearing all recorded calls: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to clear all recorded calls", err.Error())
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All recorded calls cleared successfully"})
}

func (h *VerificationHandler) handleGetExpectationStatus(w http.ResponseWriter, r *http.Request, expectationID string) {
	if expectationID == "" {
		writeErrorResponse(w, http.StatusBadRequest, "Expectation ID is required in path")
		return
	}

	// Fetch the core.Expectation to access its ExpectedCallTimes field
	coreExp, err := h.store.GetExpectation(r.Context(), expectationID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) { //
			writeErrorResponse(w, http.StatusNotFound, "Expectation not found", err.Error())
		} else {
			log.Printf("httphandler: Error getting expectation %s for status: %v", expectationID, err)
			writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve expectation status", err.Error())
		}
		return
	}

	matchCount, err := h.store.GetMatchCount(r.Context(), expectationID)
	if err != nil {
		// This case should ideally not happen if GetExpectation succeeded, but handle defensively.
		log.Printf("httphandler: Error getting match count for expectation %s: %v", expectationID, err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get match count for expectation", err.Error())
		return
	}

	isSatisfied := true                   // Default to true if no ExpectedCallTimes defined
	if coreExp.ExpectedCallTimes != nil { // Use coreExp here
		isSatisfied = coreExp.ExpectedCallTimes.IsSatisfied(matchCount) //
	}

	statusResponse := map[string]interface{}{
		"expectationId": expectationID,
		"matchCount":    matchCount,
		"isSatisfied":   isSatisfied,
		"expectedCalls": coreExp.ExpectedCallTimes, // Send the core definition
	}
	writeJSONResponse(w, http.StatusOK, statusResponse)
}
