package httphandler

import (
	"encoding/json"
	"log"
	"net/http"
)

type errorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("httphandler: Error encoding JSON response: %v", err)
			// Attempt to write a simpler error message if encoding data fails
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(errorResponse{Error: "Failed to encode response"})
		}
	}
}

func writeErrorResponse(w http.ResponseWriter, statusCode int, message string, details ...string) {
	resp := errorResponse{Error: message}
	if len(details) > 0 {
		resp.Details = details[0]
	}
	log.Printf("httphandler: Error response: status=%d, message=%s, details=%s", statusCode, message, resp.Details)
	writeJSONResponse(w, statusCode, resp)
}
