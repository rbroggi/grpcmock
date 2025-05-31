package storage

import (
	"fmt"
	"log"
	"reflect"
	"sync"

	"github.com/google/uuid"
	"github.com/rbroggi/grpcmock/internal/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	// DefaultMarshaler can be configured if needed
	DefaultMarshaler = protojson.MarshalOptions{EmitUnpopulated: true}
	// DefaultUnmarshaler can be configured if needed
	DefaultUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
	// ErrAlreadyExist is returned when an expectation with the same ID already exists.
	ErrAlreadyExist = fmt.Errorf("expectation already exists")
)

// Store holds expectations and recorded calls in memory.
type Store struct {
	expectationsByID map[string]*runtime.GRPCCallExpectation // id -> expectation
	matchCounts      map[string]int                          // key: expectation ID
	mu               sync.RWMutex
}

// New creates a new Store instance.
func New() *Store {
	return &Store{
		expectationsByID: make(map[string]*runtime.GRPCCallExpectation),
		matchCounts:      make(map[string]int),
	}
}

// CreateExpectation adds a new gRPC call expectation, returns its ID or ErrAlreadyExist.
func (s *Store) CreateExpectation(exp runtime.GRPCCallExpectation) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp.FullMethodName == "" {
		return "", fmt.Errorf("fullMethodName is required in expectation")
	}
	if exp.Response == nil && exp.StreamMock == nil {
		return "", fmt.Errorf("response or streamMock is required in expectation")
	}
	// Check for identical expectation
	for _, existing := range s.expectationsByID {
		if expectationsEqual(existing, &exp) {
			return existing.ID, ErrAlreadyExist
		}
	}
	// Assign a new UUID
	exp.ID = uuid.NewString()
	s.expectationsByID[exp.ID] = &exp
	log.Printf("grpcmockruntime: Added expectation for %s with id %s", exp.FullMethodName, exp.ID)
	return exp.ID, nil
}

// GetExpectationByID returns the expectation by its ID.
func (s *Store) GetExpectationByID(id string) (*runtime.GRPCCallExpectation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.expectationsByID[id]
	return exp, ok
}

// ListExpectations returns all expectations, optionally filtered by options.
func (s *Store) ListExpectations(opts runtime.ListExpectationsOptions) []runtime.GRPCCallExpectation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []runtime.GRPCCallExpectation
	for _, exp := range s.expectationsByID {
		if opts.FullMethodName != "" && exp.FullMethodName != opts.FullMethodName {
			continue
		}
		result = append(result, *exp)
	}
	return result
}

// DeleteExpectation removes an expectation by its ID. Returns true if deleted, false if not found.
func (s *Store) DeleteExpectation(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.expectationsByID[id]; ok {
		delete(s.expectationsByID, id)
		delete(s.matchCounts, id)
		return true
	}
	return false
}

// ClearExpectations removes all expectations and their match counts from the store.
func (s *Store) ClearExpectations() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expectationsByID = make(map[string]*runtime.GRPCCallExpectation)
	s.matchCounts = make(map[string]int)
}

// IncrementMatch increments the match count for a given expectation ID.
func (s *Store) IncrementMatch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matchCounts[id]++
}

// GetMatches returns the match count for a given expectation ID.
func (s *Store) GetMatches(id string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.matchCounts[id]
}

// expectationsEqual checks if two GRPCCallExpectation are identical (ignoring ID and ExpectedCalledTimes).
func expectationsEqual(a, b *runtime.GRPCCallExpectation) bool {
	if a == nil || b == nil {
		return false
	}
	aCopy := *a
	bCopy := *b
	aCopy.ID = ""
	bCopy.ID = ""
	aCopy.ExpectedCalledTimes = nil
	return reflect.DeepEqual(aCopy, bCopy)
}
