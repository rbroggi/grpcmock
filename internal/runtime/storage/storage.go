package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rbroggi/grpcmock/internal/runtime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	// DefaultMarshaler can be configured if needed
	DefaultMarshaler = protojson.MarshalOptions{EmitUnpopulated: true}
	// DefaultUnmarshaler can be configured if needed
	DefaultUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Store holds expectations and recorded calls in memory.
type Store struct {
	expectationsStore map[string][]runtime.GRPCCallExpectation
	recordedCalls     []runtime.RecordedGRPCCall
	mu                sync.RWMutex
}

// New creates a new Store instance.
func New() *Store {
	return &Store{
		expectationsStore: make(map[string][]runtime.GRPCCallExpectation),
		recordedCalls:     make([]runtime.RecordedGRPCCall, 0),
	}
}

// AddExpectation adds a new gRPC call expectation.
func (s *Store) AddExpectation(exp runtime.GRPCCallExpectation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp.FullMethodName == "" {
		return fmt.Errorf("fullMethodName is required in expectation")
	}
	if exp.Response == nil {
		return fmt.Errorf("response is required in expectation")
	}
	s.expectationsStore[exp.FullMethodName] = append(s.expectationsStore[exp.FullMethodName], exp)
	log.Printf("grpcmockruntime: Added expectation for %s", exp.FullMethodName)
	return nil
}

// GetExpectations returns all current expectations.
func (s *Store) GetExpectations() map[string][]runtime.GRPCCallExpectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to avoid external modification issues if the caller modifies the map/slice
	copiedExpectations := make(map[string][]runtime.GRPCCallExpectation)
	for k, v := range s.expectationsStore {
		copiedExpectations[k] = append([]runtime.GRPCCallExpectation(nil), v...)
	}
	return copiedExpectations
}

// ClearAll clears all expectations and recorded calls.
func (s *Store) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expectationsStore = make(map[string][]runtime.GRPCCallExpectation)
	s.recordedCalls = make([]runtime.RecordedGRPCCall, 0)
	log.Println("grpcmockruntime: All expectations and recorded calls cleared.")
}

// RecordCall records an incoming gRPC call.
// It now correctly uses proto.Message with protojson.Marshal.
func (s *Store) RecordCall(fullMethodName string, headers map[string][]string, reqBodyProto proto.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	s.recordedCalls = append(s.recordedCalls, runtime.RecordedGRPCCall{
		FullMethodName: fullMethodName,
		Headers:        headers,
		Body:           reqBodyJSON,
		Timestamp:      time.Now().UnixNano(),
	})
	log.Printf("grpcmockruntime: Recorded call to %s", fullMethodName) // Optional: for verbose logging
}

// GetRecordedCalls returns all recorded calls.
func (s *Store) GetRecordedCalls() []runtime.RecordedGRPCCall {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy
	return append([]runtime.RecordedGRPCCall(nil), s.recordedCalls...)
}
