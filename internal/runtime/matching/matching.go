package matching

import (
	"context"
	"encoding/json"
	"fmt"
	"log" // In production, use a structured logger
	"reflect"
	"regexp"
	"strings"

	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Store defines the interface for storing and retrieving expectations and recorded calls.
type store interface {
	ListExpectations(ctx context.Context, fullMethodNameFilter string) ([]*core.Expectation, error)
	GetMatchCount(ctx context.Context, expectationID string) (int, error)
}

type Service struct {
	store store
}

// NewService creates a new matching service.
func NewService(store store) *Service {
	return &Service{store: store}
}

// FindMatchingExpectation implements the matching.Service interface.
func (s *Service) FindMatchingExpectation(
	ctx context.Context,
	fullMethodName string,
	headers metadata.MD,
	requestBody proto.Message,
	streamType core.ExpectationType,
) (*core.Expectation, int, error) {

	expectations, err := s.store.ListExpectations(ctx, fullMethodName)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list expectations: %w", err)
	}

	var actualBodyMap map[string]interface{}
	if requestBody != nil && !reflect.ValueOf(requestBody).IsNil() {
		actualBodyMap, err = convertProtoToMap(requestBody)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to convert actual request body to map: %w", err)
		}
	}

	for _, exp := range expectations {
		// Preliminary filter by type (unary expectation for unary call, etc.)
		// Though streamType from call might be just UNARY for initial server/bidi stream setup.
		// The expectation's Type field is the primary determinant.
		if exp.Type != streamType {
			// This check needs refinement. A unary call might initiate a server stream.
			// The `streamType` parameter indicates the RPC kind from the method descriptor.
			// `exp.Type` is what the user configured for this expectation.
			// A UNARY `streamType` could match `core.ExpectationServerStream` if `exp.RequestCondition` matches.
			// A SERVER_STREAM `streamType` must match `core.ExpectationServerStream`.
			// A CLIENT_STREAM `streamType` must match `core.ExpectationClientStream`.
			// A BIDI_STREAM `streamType` must match `core.ExpectationBidiStream`.
			// For now, a direct match or if exp.Type is Server/Bidi and streamType is Unary (initial call)
			isCompatibleType := exp.Type == streamType
			if !isCompatibleType && streamType == core.ExpectationUnary &&
				(exp.Type == core.ExpectationServerStream || exp.Type == core.ExpectationBidiStream) {
				isCompatibleType = true // Unary call can start server/bidi stream
			}
			if !isCompatibleType && streamType == core.ExpectationClientStream && exp.Type == core.ExpectationBidiStream {
				isCompatibleType = true // Client stream message can be part of bidi
			}

			if !isCompatibleType {
				continue
			}
		}

		// Check call limits first (if applicable and if we want to prevent over-matching preemptively)
		// This is typically checked *after* a match is found, when incrementing.
		// However, if an expectation has Max=1 and already matched once, it shouldn't match again.
		currentMatchCount, _ := s.store.GetMatchCount(ctx, exp.ID)
		if exp.ExpectedCallTimes != nil && exp.ExpectedCallTimes.Max != nil && currentMatchCount >= *exp.ExpectedCallTimes.Max {
			continue // Already matched maximum times
		}

		if s.matches(exp.RequestCondition, headers, actualBodyMap) {
			// We found a match. The caller (handler) will increment the count.
			return exp, currentMatchCount, nil
		}
	}

	return nil, 0, nil // No matching expectation found
}

func (s *Service) matches(cond core.RequestCondition, actualHeaders metadata.MD, actualBodyMap map[string]interface{}) bool {
	if !s.matchHeaders(cond.HeadersMatcher, actualHeaders) {
		return false
	}
	if !s.matchBody(cond.BodyMatcher, actualBodyMap) {
		return false
	}
	return true
}

func (s *Service) matchHeaders(expected core.HeadersMatcher, actual metadata.MD) bool {
	if expected.Fields == nil || len(expected.Fields) == 0 {
		return true // No header conditions means it matches any headers.
	}

	for key, fieldMatcher := range expected.Fields {
		headerValues := actual.Get(key) // actual is metadata.MD
		if len(headerValues) == 0 {
			return false // Expected header key not present
		}

		matchedThisHeader := false
		for _, actualValue := range headerValues {
			if s.matchFieldValue(fieldMatcher, actualValue) {
				matchedThisHeader = true
				break // Found a value that matches the condition for this header key
			}
		}
		if !matchedThisHeader {
			return false // No value for this header key satisfied the FieldMatcher
		}
	}
	return true
}

func (s *Service) matchFieldValue(matcher core.FieldMatcher, value string) bool {
	if matcher.Equals != "" {
		if matcher.Equals != value {
			return false
		}
	}
	if matcher.Contains != "" {
		if !strings.Contains(value, matcher.Contains) {
			return false
		}
	}
	if matcher.Regex != "" {
		matched, err := regexp.MatchString(matcher.Regex, value)
		if err != nil {
			log.Printf("WARN: Regex compilation error for pattern '%s': %v", matcher.Regex, err)
			return false // Invalid regex pattern should not match
		}
		if !matched {
			return false
		}
	}
	// If all specified conditions pass (or if matcher is empty, implying wildcard for this field if it exists)
	return true
}

func (s *Service) matchBody(expected core.BodyMatcher, actualBodyMap map[string]interface{}) bool {
	switch expected.Strategy {
	case core.BodyMatchUndefined: // No specific body matching strategy
		return true
	case core.BodyMatchFull:
		if expected.ExpectedBody == nil && actualBodyMap == nil {
			return true
		}
		if expected.ExpectedBody == nil && actualBodyMap != nil && len(actualBodyMap) == 0 {
			return true
		} // Empty actual map
		if (expected.ExpectedBody == nil) != (actualBodyMap == nil) && (len(expected.ExpectedBody) > 0 || len(actualBodyMap) > 0) {
			return false // one is nil/empty, the other is not
		}
		return reflect.DeepEqual(expected.ExpectedBody, actualBodyMap)
	case core.BodyMatchSubset:
		if expected.ExpectedBody == nil { // An empty subset definition matches any body
			return true
		}
		return isSubset(expected.ExpectedBody, actualBodyMap)
	default:
		log.Printf("WARN: Unknown body matching strategy: %s", expected.Strategy)
		return false
	}
}

// convertProtoToMap converts a proto.Message to map[string]interface{} using protojson.
// This is the canonical representation for matching.
func convertProtoToMap(p proto.Message) (map[string]interface{}, error) {
	if p == nil || reflect.ValueOf(p).IsNil() {
		return nil, nil // Or an empty map: make(map[string]interface{}), depending on desired behavior for nil body
	}

	marshaler := protojson.MarshalOptions{
		UseProtoNames:   false, // Standard camelCase
		EmitUnpopulated: true,  // So that zero values are present for comparison
	}
	jsonBytes, err := marshaler.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("protojson marshal error: %w", err)
	}

	if len(jsonBytes) == 0 || string(jsonBytes) == "{}" || string(jsonBytes) == "null" {
		// If proto message is empty (e.g. google.protobuf.Empty), result can be "{}".
		// An empty map is a valid representation.
		return make(map[string]interface{}), nil
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &resultMap); err != nil {
		return nil, fmt.Errorf("json unmarshal to map error: %w (json: %s)", err, string(jsonBytes))
	}
	return resultMap, nil
}

// isSubset recursively checks if 'subsetCandidate' is a JSON-like subset of 'supersetCandidate'.
// Both arguments are expected to be map[string]interface{} from parsed JSON.
func isSubset(subsetCandidate, supersetCandidate map[string]interface{}) bool {
	if subsetCandidate == nil { // An empty or nil subset is technically a subset of anything.
		return true
	}
	if supersetCandidate == nil { // A non-nil subset cannot be a subset of nil.
		return subsetCandidate == nil // or len(subsetCandidate) == 0
	}

	for key, subValue := range subsetCandidate {
		superValue, ok := supersetCandidate[key]
		if !ok {
			return false // Key from subset is missing in superset
		}

		// Now compare subValue and superValue
		subMap, subIsMap := subValue.(map[string]interface{})
		superMap, superIsMap := superValue.(map[string]interface{})

		_, subIsSlice := subValue.([]interface{})
		_, superIsSlice := superValue.([]interface{})

		if subIsMap && superIsMap {
			// If both are maps, recurse
			if !isSubset(subMap, superMap) {
				return false
			}
		} else if subIsSlice && superIsSlice {
			// If both are slices, for subset, we might require exact match of the slice,
			// or that superSlice "contains" subSlice elements in some way.
			// For now, reflect.DeepEqual implies an exact match for slices.
			// A true array subset (e.g. [1,2] is subset of [1,2,3] but also of [3,1,2]) is more complex.
			// Let's assume for "subset" of JSON objects, if a field is an array, that array must be identical.
			if !reflect.DeepEqual(subValue, superValue) {
				return false
			}
		} else if subIsMap != superIsMap || subIsSlice != superIsSlice {
			// Type mismatch at this key (one is a map/slice, the other is not of the same collection type)
			return false
		} else {
			// Neither are maps nor slices (or both are of different types where one is not map/slice),
			// compare directly (handles primitives).
			if !reflect.DeepEqual(subValue, superValue) {
				return false
			}
		}
	}
	return true // All keys and values in subsetCandidate were found and matched in supersetCandidate
}
