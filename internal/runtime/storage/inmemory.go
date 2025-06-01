package storage

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/uuid"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
)

// InMemoryStore is an in-memory implementation of the storage.Store interface.
type InMemoryStore struct {
	mu sync.RWMutex

	expectationsByID map[string]*core.Expectation
	expectationsAll  []*core.Expectation // For ordered listing, if needed, or quick iteration

	callsByID map[string]*core.RecordedCall
	callsAll  []*core.RecordedCall

	matchCountsByExpectationID map[string]int
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		expectationsByID:           make(map[string]*core.Expectation),
		expectationsAll:            make([]*core.Expectation, 0),
		callsByID:                  make(map[string]*core.RecordedCall),
		callsAll:                   make([]*core.RecordedCall, 0),
		matchCountsByExpectationID: make(map[string]int),
	}
}

// --- Expectations ---

func (s *InMemoryStore) CreateExpectation(_ context.Context, exp *core.Expectation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if exp.ID == "" {
		exp.ID = uuid.NewString()
	} else {
		if _, exists := s.expectationsByID[exp.ID]; exists {
			return fmt.Errorf("expectation with ID '%s' already exists: %w", exp.ID, ErrAlreadyExists)
		}
	}

	// Check for functional duplicates (ignoring ID and call times for "sameness")
	// This is a simple check; more sophisticated might be needed if order matters or IDs are client-set.
	for _, existingExp := range s.expectationsByID {
		if expectationsAreFunctionallyEqual(existingExp, exp) {
			// Return an error with the ID of the existing functionally identical expectation
			return fmt.Errorf("functionally identical expectation already exists with ID '%s': %w", existingExp.ID, ErrAlreadyExists)
		}
	}

	newExpCopy := deepCopyExpectation(exp)
	s.expectationsByID[newExpCopy.ID] = newExpCopy
	s.expectationsAll = append(s.expectationsAll, newExpCopy) // Note: order might not be guaranteed if deletions happen often

	return nil
}

func (s *InMemoryStore) GetExpectation(_ context.Context, id string) (*core.Expectation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exp, exists := s.expectationsByID[id]
	if !exists {
		return nil, ErrNotFound
	}
	return deepCopyExpectation(exp), nil
}

func (s *InMemoryStore) ListExpectations(_ context.Context, fullMethodNameFilter string) ([]*core.Expectation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*core.Expectation
	for _, exp := range s.expectationsAll { // Iterate s.expectationsAll to maintain some semblance of insertion order (if no deletes)
		if fullMethodNameFilter == "" || exp.FullMethodName == fullMethodNameFilter {
			result = append(result, deepCopyExpectation(s.expectationsByID[exp.ID])) // ensure we get the latest from map
		}
	}
	return result, nil
}

func (s *InMemoryStore) UpdateExpectation(_ context.Context, exp *core.Expectation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if exp.ID == "" {
		return fmt.Errorf("expectation ID is required for update: %w", ErrInvalidInput)
	}
	_, exists := s.expectationsByID[exp.ID]
	if !exists {
		return fmt.Errorf("expectation with ID '%s' not found for update: %w", exp.ID, ErrNotFound)
	}

	// Update in map
	s.expectationsByID[exp.ID] = deepCopyExpectation(exp)

	// Update in slice (less efficient, implies slice is more for ordered iteration)
	for i, e := range s.expectationsAll {
		if e.ID == exp.ID {
			s.expectationsAll[i] = deepCopyExpectation(exp)
			break
		}
	}
	return nil
}

func (s *InMemoryStore) DeleteExpectation(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.expectationsByID[id]; !exists {
		return ErrNotFound
	}
	delete(s.expectationsByID, id)

	// Remove from slice
	var updatedAll []*core.Expectation
	for _, exp := range s.expectationsAll {
		if exp.ID != id {
			updatedAll = append(updatedAll, exp)
		}
	}
	s.expectationsAll = updatedAll
	delete(s.matchCountsByExpectationID, id) // Also clear match count

	return nil
}

func (s *InMemoryStore) ClearAllExpectations(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expectationsByID = make(map[string]*core.Expectation)
	s.expectationsAll = make([]*core.Expectation, 0)
	s.matchCountsByExpectationID = make(map[string]int) // Clear all match counts too
	return nil
}

// --- Recorded Calls ---

func (s *InMemoryStore) RecordCall(
	_ context.Context,
	call *core.RecordedCall,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if call.ID == "" {
		call.ID = uuid.NewString()
	} else {
		if _, exists := s.callsByID[call.ID]; exists {
			return fmt.Errorf("call with ID '%s' already recorded: %w", call.ID, ErrAlreadyExists)
		}
	}
	newCallCopy := deepCopyCall(call)
	s.callsByID[newCallCopy.ID] = newCallCopy
	s.callsAll = append(s.callsAll, newCallCopy)
	return nil
}

func (s *InMemoryStore) ListCalls(_ context.Context, fullMethodNameFilter string) ([]*core.RecordedCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*core.RecordedCall
	for _, call := range s.callsAll {
		if fullMethodNameFilter == "" || call.FullMethodName == fullMethodNameFilter {
			result = append(result, deepCopyCall(s.callsByID[call.ID]))
		}
	}
	return result, nil
}

func (s *InMemoryStore) GetCall(_ context.Context, id string) (*core.RecordedCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	call, exists := s.callsByID[id]
	if !exists {
		return nil, ErrNotFound
	}
	return deepCopyCall(call), nil
}

func (s *InMemoryStore) ClearAllCalls(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callsByID = make(map[string]*core.RecordedCall)
	s.callsAll = make([]*core.RecordedCall, 0)
	return nil
}

// --- Match Counts ---

func (s *InMemoryStore) IncrementMatchCount(_ context.Context, expectationID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure the expectation exists before incrementing its count
	if _, exists := s.expectationsByID[expectationID]; !exists {
		return 0, fmt.Errorf("cannot increment match count for non-existent expectation ID '%s': %w", expectationID, ErrNotFound)
	}

	s.matchCountsByExpectationID[expectationID]++
	return s.matchCountsByExpectationID[expectationID], nil
}

func (s *InMemoryStore) GetMatchCount(_ context.Context, expectationID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// It's okay to get a count for an expectation that might have been deleted after matching,
	// or to get 0 if it never matched or doesn't exist.
	return s.matchCountsByExpectationID[expectationID], nil
}

func (s *InMemoryStore) ClearAllMatchCounts(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matchCountsByExpectationID = make(map[string]int)
	return nil
}

// --- Helpers ---

func deepCopyExpectation(original *core.Expectation) *core.Expectation {
	if original == nil {
		return nil
	}
	// Using reflect.Copy for simplicity here; for production, consider specific copy logic
	// or a library if performance with reflect is an issue, or if types are complex.
	// This is a shallow copy for map/slice fields within the struct if not careful.
	// For a truly deep copy, each field would need to be copied appropriately.
	// For now, this basic copy handles top-level fields.
	// A more robust deep copy would involve manually copying nested structures.
	cpy := *original

	// Manual deep copy for ExpectedBody map
	if original.RequestCondition.BodyMatcher.ExpectedBody != nil {
		cpy.RequestCondition.BodyMatcher.ExpectedBody = deepCopyMap(original.RequestCondition.BodyMatcher.ExpectedBody)
	}
	if original.ResponseAction.Body != nil {
		cpy.ResponseAction.Body = deepCopyMap(original.ResponseAction.Body)
	}
	if original.RequestCondition.HeadersMatcher.Fields != nil {
		cpy.RequestCondition.HeadersMatcher.Fields = deepCopyFieldMatcherMap(original.RequestCondition.HeadersMatcher.Fields)
	}
	if original.ResponseAction.Headers != nil {
		cpy.ResponseAction.Headers = deepCopyStringMap(original.ResponseAction.Headers)
	}
	if original.ExpectedCallTimes != nil {
		ectCopy := *original.ExpectedCallTimes
		if original.ExpectedCallTimes.Min != nil {
			minVal := *original.ExpectedCallTimes.Min
			ectCopy.Min = &minVal
		}
		if original.ExpectedCallTimes.Max != nil {
			maxVal := *original.ExpectedCallTimes.Max
			ectCopy.Max = &maxVal
		}
		cpy.ExpectedCallTimes = &ectCopy
	}
	if len(original.StreamActions) > 0 {
		cpy.StreamActions = make([]core.StreamAction, len(original.StreamActions))
		for i, sa := range original.StreamActions {
			newSa := core.StreamAction{}
			if sa.Send != nil {
				sendCopy := *sa.Send // Assuming ResponseAction is already deep copied by its fields
				if sa.Send.Body != nil {
					sendCopy.Body = deepCopyMap(sa.Send.Body)
				}
				if sa.Send.Headers != nil {
					sendCopy.Headers = deepCopyStringMap(sa.Send.Headers)
				}
				if sa.Send.Error != nil {
					errCopy := *sa.Send.Error
					sendCopy.Error = &errCopy
				}
				newSa.Send = &sendCopy
			}
			if sa.Receive != nil {
				// Assuming RequestCondition is handled by its fields being copied
				receiveCopy := *sa.Receive
				if sa.Receive.BodyMatcher.ExpectedBody != nil {
					receiveCopy.BodyMatcher.ExpectedBody = deepCopyMap(sa.Receive.BodyMatcher.ExpectedBody)
				}
				if sa.Receive.HeadersMatcher.Fields != nil {
					receiveCopy.HeadersMatcher.Fields = deepCopyFieldMatcherMap(sa.Receive.HeadersMatcher.Fields)
				}
				newSa.Receive = &receiveCopy
			}
			cpy.StreamActions[i] = newSa
		}
	}

	return &cpy
}

func deepCopyCall(original *core.RecordedCall) *core.RecordedCall {
	if original == nil {
		return nil
	}
	cpy := *original
	if original.RequestBody != nil {
		if mapBody, ok := original.RequestBody.(map[string]interface{}); ok {
			cpy.RequestBody = deepCopyMap(mapBody)
		}
		// if it's not a map, it might be a string or other primitive, which is fine by direct copy.
	}
	// Headers are metadata.MD, which is map[string][]string; need to deep copy.
	if original.Headers != nil {
		cpy.Headers = original.Headers.Copy()
	}
	return &cpy
}

func deepCopyMap(original map[string]interface{}) map[string]interface{} {
	if original == nil {
		return nil
	}
	newMap := make(map[string]interface{}, len(original))
	for k, v := range original {
		// This is a shallow copy for nested maps/slices within the interface value.
		// A truly generic deep copy of interface{} is complex.
		// For common JSON structures (maps, slices, primitives), this might be okay.
		// If v is a map[string]interface{}, it should be deep copied recursively.
		// If v is a []interface{}, it should be deep copied recursively.
		// For now, direct assignment. Consider libraries like github.com/mohae/deepcopy for robust deep copying.
		newMap[k] = v // Needs more robust deep copy for nested structures
	}
	return newMap
}
func deepCopyStringMap(original map[string]string) map[string]string {
	if original == nil {
		return nil
	}
	newMap := make(map[string]string, len(original))
	for k, v := range original {
		newMap[k] = v
	}
	return newMap
}
func deepCopyFieldMatcherMap(original map[string]core.FieldMatcher) map[string]core.FieldMatcher {
	if original == nil {
		return nil
	}
	newMap := make(map[string]core.FieldMatcher, len(original))
	for k, v := range original {
		newMap[k] = v
	} // FieldMatcher is struct of strings, direct copy is fine
	return newMap
}

// expectationsAreFunctionallyEqual checks if two expectations are the same in terms of matching
// and response, ignoring their ID and ExpectedCallTimes.
func expectationsAreFunctionallyEqual(e1, e2 *core.Expectation) bool {
	if e1 == nil || e2 == nil {
		return e1 == e2
	}
	// Compare key fields that define the expectation's behavior
	return e1.FullMethodName == e2.FullMethodName &&
		e1.Type == e2.Type &&
		reflect.DeepEqual(e1.RequestCondition, e2.RequestCondition) && // Assumes RequestCondition can be DeepEqual'd
		reflect.DeepEqual(e1.ResponseAction, e2.ResponseAction) && // Assumes ResponseAction can be DeepEqual'd
		reflect.DeepEqual(e1.StreamActions, e2.StreamActions) // Assumes StreamAction can be DeepEqual'd
}
