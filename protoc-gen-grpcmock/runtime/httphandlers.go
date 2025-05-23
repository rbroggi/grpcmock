package runtime

import (
	"encoding/json"
	"net/http"
)

// HandleExpectations manages HTTP requests for CRUD operations on expectations.
func HandleExpectations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var exp GRPCCallExpectation
		if err := json.NewDecoder(r.Body).Decode(&exp); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Failed to decode expectation", err)
			return
		}
		if err := AddExpectation(exp); err != nil {
			writeErrorResponse(w, http.StatusBadRequest, "Invalid expectation", err)
			return
		}
		writeJSONResponse(w, http.StatusCreated, map[string]string{"message": "Expectation added"})
	case http.MethodGet:
		writeJSONResponse(w, http.StatusOK, GetExpectations())
	case http.MethodDelete:
		ClearAll() // Clears both expectations and recorded calls
		writeJSONResponse(w, http.StatusOK, map[string]string{"message": "All expectations and recorded calls cleared"})
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed", nil)
	}
}

// HandleVerifications manages HTTP requests for retrieving recorded calls.
func HandleVerifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSONResponse(w, http.StatusOK, GetRecordedCalls())
	default:
		writeErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed", nil)
	}
}
