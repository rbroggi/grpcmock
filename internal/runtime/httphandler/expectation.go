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

// expectationStore defines the methods from storage.Store needed by expectation handlers.
type expectationStore interface {
	CreateExpectation(ctx context.Context, exp *core.Expectation) error
	GetExpectation(ctx context.Context, id string) (*core.Expectation, error)
	ListExpectations(ctx context.Context, fullMethodNameFilter string) ([]*core.Expectation, error)
	DeleteExpectation(ctx context.Context, id string) error
	ClearAllExpectations(ctx context.Context) error
	ClearAllMatchCounts(ctx context.Context) error // Match counts are related
}

// ExpectationHandler handles HTTP requests for managing expectations.
type ExpectationHandler struct {
	store expectationStore
}

// NewExpectationHandler creates a new ExpectationHandler.
func NewExpectationHandler(s expectationStore) *ExpectationHandler {
	return &ExpectationHandler{store: s}
}

// ServeHTTP routes requests for /expectations and /expectations/{id}.
func (h *ExpectationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	trimmedPath := strings.TrimPrefix(r.URL.Path, "/expectations")
	id := strings.Trim(trimmedPath, "/")

	switch r.Method {
	case http.MethodPost:
		if id == "" { // POST /expectations
			h.handleCreateExpectation(w, r)
		} else {
			writeErrorResponse(w, http.StatusMethodNotAllowed, "POST not allowed for /expectations/{id}")
		}
	case http.MethodGet:
		if id == "" { // GET /expectations
			h.handleListExpectations(w, r)
		} else { // GET /expectations/{id}
			h.handleGetExpectationByID(w, r, id)
		}
	case http.MethodDelete:
		if id == "" { // DELETE /expectations
			h.handleClearAllExpectations(w, r)
		} else { // DELETE /expectations/{id}
			h.handleDeleteExpectationByID(w, r, id)
		}
		// case http.MethodPut: // PUT /expectations/{id}
		// 	if id != "" {
		// 		// h.handleUpdateExpectation(w, r, id) // TODO: Implement if needed
		// 	} else {
		// 		writeErrorResponse(w, http.StatusMethodNotAllowed, "PUT requires an expectation ID")
		// 	}
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *ExpectationHandler) handleCreateExpectation(w http.ResponseWriter, r *http.Request) {
	var apiExp api.GRPCCallExpectation                              //
	if err := json.NewDecoder(r.Body).Decode(&apiExp); err != nil { //
		writeErrorResponse(w, http.StatusBadRequest, "Failed to decode expectation JSON", err.Error())
		return
	}

	coreExp, err := acl.ToInternalExpectation(&apiExp) //
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, "Invalid expectation data", err.Error())
		return
	}

	if err := h.store.CreateExpectation(r.Context(), coreExp); err != nil { //
		if errors.Is(err, storage.ErrAlreadyExists) { //
			errMsg := err.Error()
			var existingID string
			if _, scanErr := fmt.Sscanf(errMsg, "functionally identical expectation already exists with ID '%s'", &existingID); scanErr == nil { //
				writeJSONResponse(w, http.StatusConflict, map[string]string{"id": existingID, "error": "Functionally identical expectation already exists"})
			} else if _, scanErr := fmt.Sscanf(errMsg, "expectation with ID '%s' already exists", &existingID); scanErr == nil { //
				writeJSONResponse(w, http.StatusConflict, map[string]string{"id": existingID, "error": "Expectation with this ID already exists"})
			} else {
				writeErrorResponse(w, http.StatusConflict, "Expectation already exists or is functionally identical.", err.Error())
			}
			return
		}
		log.Printf("httphandler: Error creating expectation: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to create expectation", err.Error())
		return
	}

	writeJSONResponse(w, http.StatusCreated, api.CreateExpectationResponse{ID: coreExp.ID}) //
}

func (h *ExpectationHandler) handleListExpectations(w http.ResponseWriter, r *http.Request) {
	fullMethodNameFilter := r.URL.Query().Get("fullMethodName")

	coreExps, err := h.store.ListExpectations(r.Context(), fullMethodNameFilter)
	if err != nil {
		log.Printf("httphandler: Error listing expectations: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to list expectations", err.Error())
		return
	}

	apiExps := make([]*api.GRPCCallExpectation, 0, len(coreExps)) //
	for _, coreExp := range coreExps {
		apiExp, errConv := acl.ToAPIExpectation(coreExp) //
		if errConv != nil {
			log.Printf("httphandler: Error translating core expectation to API DTO (ID: %s): %v", coreExp.ID, errConv)
			continue //
		}
		apiExps = append(apiExps, apiExp)
	}

	writeJSONResponse(w, http.StatusOK, apiExps)
}

func (h *ExpectationHandler) handleClearAllExpectations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.store.ClearAllExpectations(ctx); err != nil { //
		log.Printf("httphandler: Error clearing all expectations: %v", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to clear all expectations", err.Error())
		return
	}
	if err := h.store.ClearAllMatchCounts(ctx); err != nil { //
		log.Printf("httphandler: Error clearing all match counts: %v", err)
		// Non-fatal for the expectations clear operation itself
	}
	writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All expectations cleared successfully"})
}

func (h *ExpectationHandler) handleGetExpectationByID(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "Expectation ID is required in path")
		return
	}
	coreExp, err := h.store.GetExpectation(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) { //
			writeErrorResponse(w, http.StatusNotFound, "Expectation not found", err.Error())
		} else {
			log.Printf("httphandler: Error getting expectation %s: %v", id, err)
			writeErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve expectation", err.Error())
		}
		return
	}
	apiExp, errConv := acl.ToAPIExpectation(coreExp) //
	if errConv != nil {
		log.Printf("httphandler: Error translating core expectation to API DTO for GetByID (ID: %s): %v", coreExp.ID, errConv)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to process expectation data", errConv.Error())
		return
	}
	writeJSONResponse(w, http.StatusOK, apiExp)
}

func (h *ExpectationHandler) handleDeleteExpectationByID(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeErrorResponse(w, http.StatusBadRequest, "Expectation ID is required in path")
		return
	}
	err := h.store.DeleteExpectation(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) { //
			writeErrorResponse(w, http.StatusNotFound, "Expectation not found for deletion", err.Error())
		} else {
			log.Printf("httphandler: Error deleting expectation %s: %v", id, err)
			writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete expectation", err.Error())
		}
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Expectation %s deleted successfully", id)})
}
