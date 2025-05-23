package runtime

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	expectationsStore = make(map[string][]GRPCCallExpectation)
	recordedCalls     = make([]RecordedGRPCCall, 0)
	mu                sync.RWMutex

	// DefaultMarshaler can be configured if needed
	DefaultMarshaler = protojson.MarshalOptions{EmitUnpopulated: true}
	// DefaultUnmarshaler can be configured if needed
	DefaultUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// AddExpectation adds a new gRPC call expectation.
func AddExpectation(exp GRPCCallExpectation) error {
	mu.Lock()
	defer mu.Unlock()
	if exp.FullMethodName == "" {
		return fmt.Errorf("fullMethodName is required in expectation")
	}
	if exp.Response == nil {
		return fmt.Errorf("response is required in expectation")
	}
	expectationsStore[exp.FullMethodName] = append(expectationsStore[exp.FullMethodName], exp)
	log.Printf("grpcmockruntime: Added expectation for %s", exp.FullMethodName)
	return nil
}

// GetExpectations returns all current expectations.
func GetExpectations() map[string][]GRPCCallExpectation {
	mu.RLock()
	defer mu.RUnlock()
	// Return a copy to avoid external modification issues if the caller modifies the map/slice
	copiedExpectations := make(map[string][]GRPCCallExpectation)
	for k, v := range expectationsStore {
		copiedExpectations[k] = append([]GRPCCallExpectation(nil), v...)
	}
	return copiedExpectations
}

// ClearAll clears all expectations and recorded calls.
func ClearAll() {
	mu.Lock()
	defer mu.Unlock()
	expectationsStore = make(map[string][]GRPCCallExpectation)
	recordedCalls = make([]RecordedGRPCCall, 0)
	log.Println("grpcmockruntime: All expectations and recorded calls cleared.")
}

// RecordCall records an incoming gRPC call.
// It now correctly uses proto.Message with protojson.Marshal.
func RecordCall(fullMethodName string, headers map[string][]string, reqBodyProto proto.Message) {
	mu.Lock()
	defer mu.Unlock()

	var reqBodyJSON json.RawMessage = []byte("{}") // Default to empty JSON if reqBodyProto is nil or marshalling fails

	if reqBodyProto != nil {
		bytes, err := DefaultMarshaler.Marshal(reqBodyProto) // Directly use reqBodyProto (which is proto.Message)
		if err != nil {
			// Log the error but still proceed to record the call, possibly with an empty or error indicator in the body
			log.Printf("grpcmockruntime: error marshalling request body to JSON for recording call '%s': %v", fullMethodName, err)
			// Optionally, you could store an error message in reqBodyJSON or a separate field
			errorMsg := fmt.Sprintf(`{"error_marshalling_request_body": "%s"}`, err.Error())
			reqBodyJSON = json.RawMessage(errorMsg)
		} else {
			reqBodyJSON = json.RawMessage(bytes)
		}
	}

	recordedCalls = append(recordedCalls, RecordedGRPCCall{
		FullMethodName: fullMethodName,
		Headers:        headers,
		Body:           reqBodyJSON,
		Timestamp:      time.Now().UnixNano(),
	})
	log.Printf("grpcmockruntime: Recorded call to %s", fullMethodName) // Optional: for verbose logging
}

// GetRecordedCalls returns all recorded calls.
func GetRecordedCalls() []RecordedGRPCCall {
	mu.RLock()
	defer mu.RUnlock()
	// Return a copy
	return append([]RecordedGRPCCall(nil), recordedCalls...)
}

// --- HTTP Response Helpers ---

func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("grpcmockruntime: Failed to write JSON response: %v", err)
			// Attempt to write a plain text error if JSON encoding fails
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		}
	}
}

func writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	log.Printf("grpcmockruntime: HTTP Error %d: %s (%v)", statusCode, message, err)
	w.Header().Set("Content-Type", "application/json") // Or text/plain
	w.WriteHeader(statusCode)
	errMsg := map[string]string{"error": message}
	if err != nil {
		errMsg["detail"] = err.Error()
	}
	json.NewEncoder(w).Encode(errMsg)
}
