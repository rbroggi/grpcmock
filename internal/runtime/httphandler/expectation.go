package httphandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/rbroggi/grpcmock/internal/runtime/acl"
	"github.com/rbroggi/grpcmock/internal/runtime/api"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"github.com/rbroggi/grpcmock/internal/runtime/storage"
)

type expectationStore interface {
	CreateExpectation(ctx context.Context, exp *core.Expectation) error
	ListExpectations(ctx context.Context, fullMethodNameFilter string) ([]*core.Expectation, error)
	ClearAllExpectations(ctx context.Context) error
	ClearAllMatchCounts(ctx context.Context) error
}

// Expectation handles HTTP requests for managing expectations.
type Expectation struct {
	store expectationStore
}

// NewExpectationHandler creates a new Expectation.
func NewExpectationHandler(s expectationStore) *Expectation {
	return &Expectation{store: s}
}

// ServeHTTP routes requests to appropriate handlers.
// Base path: /expectations
func (h *Expectation) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	trimmedPath := strings.TrimPrefix(r.URL.Path, "/expectations")
	trimmedPath = strings.Trim(trimmedPath, "/")

	// Routing:
	// POST /expectations -> Create
	// GET  /expectations -> List
	// DELETE /expectations -> ClearAll
	// GET /expectations/{id} -> GetByID
	// DELETE /expectations/{id} -> DeleteByID
	// PUT /expectations/{id} -> UpdateByID (optional)

	switch r.Method {
	case http.MethodPost:
		if trimmedPath == "" {
			h.handleCreateExpectation(w, r)
		} else {
			writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed for this path")
		}
	case http.MethodGet:
		if trimmedPath == "" {
			h.handleListExpectations(w, r)
		} else {
			// Potentially /expectations/{id}
			// h.handleGetExpectationByID(w, r, trimmedPath)
			writeErrorResponse(w, http.StatusNotFound, "Specific expectation GET not yet implemented or path invalid")
		}
	case http.MethodDelete:
		if trimmedPath == "" {
			h.handleClearAllExpectations(w, r)
		} else {
			// Potentially /expectations/{id} for deleting one
			// h.handleDeleteExpectationByID(w, r, trimmedPath)
			writeErrorResponse(w, http.StatusNotFound, "Specific expectation DELETE not yet implemented or path invalid")
		}
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Expectation) handleCreateExpectation(w http.ResponseWriter, r *http.Request) {
	var apiExp api.GRPCCallExpectation
	if err := json.NewDecoder(r.Body).Decode(&apiExp); err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "Failed to decode expectation JSON", err.Error())
		return
	}

	coreExp, err := acl.ToInternalExpectation(&apiExp)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "Invalid expectation data", err.Error())
		return
	}

	if err := h.store.CreateExpectation(r.Context(), coreExp); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			// Extract the ID from the error message if possible, or use the input ID
			errMsg := err.Error()
			var existingID string
			if _, scanErr := fmt.Sscanf(errMsg, "functionally identical expectation already exists with ID '%s'", &existingID); scanErr == nil {
				writeJSONResponse(w, http.StatusConflict, map[string]string{"id": existingID, "error": "Functionally identical expectation already exists"})
			} else if _, scanErr := fmt.Sscanf(errMsg, "expectation with ID '%s' already exists", &existingID); scanErr == nil {
				writeJSONResponse(w, http.StatusConflict, map[string]string{"id": existingID, "error": "Expectation with this ID already exists"})
			} else {
				writeErrorResponse(w, http.StatusConflict, "Expectation already exists", err.Error())
			}
			return
		}
		log.Printf("httphandler: Error creating expectation: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to create expectation", err.Error())
		return
	}

	writeJSONResponse(w, http.StatusCreated, api.CreateExpectationResponse{ID: coreExp.ID})
}

func (h *Expectation) handleListExpectations(w http.ResponseWriter, r *http.Request) {
	// Optional: filter by fullMethodName query param
	fullMethodNameFilter := r.URL.Query().Get("fullMethodName")

	coreExps, err := h.store.ListExpectations(r.Context(), fullMethodNameFilter)
	if err != nil {
		log.Printf("httphandler: Error listing expectations: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to list expectations", err.Error())
		return
	}

	apiExps := make([]*api.GRPCCallExpectation, 0, len(coreExps))
	for _, coreExp := range coreExps {
		apiExp, err := acl.ToAPIExpectation(coreExp)
		if err != nil {
			log.Printf("httphandler: Error translating core expectation to API DTO (ID: %s): %v", coreExp.ID, err)
			// Skip this one in the response or return an error for the whole list?
			// For now, skip.
			continue
		}
		apiExps = append(apiExps, apiExp)
	}

	writeJSONResponse(w, http.StatusOK, apiExps)
}

func (h *Expectation) handleClearAllExpectations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.store.ClearAllExpectations(ctx); err != nil {
		log.Printf("httphandler: Error clearing all expectations: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to clear all expectations", err.Error())
		return
	}
	// Also clear match counts when all expectations are cleared.
	if err := h.store.ClearAllMatchCounts(ctx); err != nil {
		log.Printf("httphandler: Error clearing all match counts: %v", err)
		// Non-fatal for the expectations clear operation
	}

	writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All expectations cleared successfully"})
}

// TODO: Implement handleGetExpectationByID, handleDeleteExpectationByID, handleUpdateExpectationByID
