package matcher

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"

	"github.com/rbroggi/grpcmock/internal/runtime"
	"github.com/rbroggi/grpcmock/internal/runtime/storage"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// storeInterface defines the methods for expectation and call storage.
type storeInterface interface {
	AddExpectation(exp runtime.GRPCCallExpectation) error
	GetExpectations() map[string][]runtime.GRPCCallExpectation
	ClearAll()
	RecordCall(fullMethodName string, headers map[string][]string, reqBodyProto proto.Message)
	GetRecordedCalls() []runtime.RecordedGRPCCall
}

func matchesRegex(pattern, text string) bool {
	if pattern == "" { // an empty pattern could mean "any value" if not specified, or exact empty string.
		return text == "" // Assuming exact empty string if pattern is empty. Define behavior as needed.
	}
	matched, err := regexp.MatchString(pattern, text)
	if err != nil {
		log.Printf("grpcmockruntime: regex error matching pattern '%s' with text '%s': %v", pattern, text, err)
		return false // Fail on invalid regex pattern
	}
	return matched
}

func isNumber(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// deepCompare compares expected and actual values.
// 'exact' means maps and slices must have the same set of elements (keys and length).
// If 'exact' is false (like a 'contains' match for maps/slices):
//   - For maps: all keys in 'expected' must be in 'actual' with matching values. 'actual' can have more keys.
//   - For slices: 'actual' must contain all elements of 'expected' in the same order. 'actual' can be longer.
//     (For unordered slice contains, more complex logic would be needed).
func deepCompare(expected, actual interface{}, exact bool) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected == nil || actual == nil {
		// If one is nil but not the other, they aren't equal.
		// Special case: if expected is an empty map/slice and actual is nil, it might be considered a match for 'contains'.
		// This depends on desired strictness. For now, let's be strict.
		return false
	}

	expVal := reflect.ValueOf(expected)
	actVal := reflect.ValueOf(actual)

	// Handle potential JSON numbers (float64) vs Go struct numbers (int, etc.)
	if isNumber(expVal.Kind()) && isNumber(actVal.Kind()) {
		// Convert both to float64 for comparison to handle type differences from JSON unmarshalling
		var fExp, fAct float64
		if expVal.CanFloat() {
			fExp = expVal.Float()
		} else if expVal.CanUint() {
			fExp = float64(expVal.Uint())
		} else { // Int
			fExp = float64(expVal.Int())
		}

		if actVal.CanFloat() {
			fAct = actVal.Float()
		} else if actVal.CanUint() {
			fAct = float64(actVal.Uint())
		} else { // Int
			fAct = float64(actVal.Int())
		}
		return fExp == fAct
	}

	if expVal.Kind() != actVal.Kind() {
		return false
	}

	switch expVal.Kind() {
	case reflect.Map:
		expMap, okE := expected.(map[string]interface{})
		actMap, okA := actual.(map[string]interface{})
		if !okE || !okA {
			return false
		}
		if exact && len(expMap) != len(actMap) {
			return false
		}
		for k, vExp := range expMap {
			vAct, ok := actMap[k]
			if !ok || !deepCompare(vExp, vAct, exact) { // Recursive call, maintain 'exact' for sub-elements
				return false
			}
		}
		return true
	case reflect.Slice:
		expSlice, okE := expected.([]interface{})
		actSlice, okA := actual.([]interface{})
		if !okE || !okA {
			return false
		}
		if exact && len(expSlice) != len(actSlice) {
			return false
		}
		if !exact && len(expSlice) > len(actSlice) { // Expected slice cannot be larger for a "contains" type match
			return false
		}
		// For slice, 'exact' means elements and order must match.
		// If !exact, it implies 'actual' must contain 'expected' as an ordered sub-sequence starting at index 0.
		// For more flexible slice matching (e.g. unordered contains), this needs enhancement.
		for i, vExp := range expSlice {
			if i >= len(actSlice) || !deepCompare(vExp, actSlice[i], true) { // Compare elements exactly
				return false
			}
		}
		return true
	default:
		return fmt.Sprintf("%v", expected) == fmt.Sprintf("%v", actual)
	}
}

// Matcher provides expectation matching using a storeInterface.
type Matcher struct {
	Store storeInterface
}

// New creates a new Matcher with the given store.
func New(store storeInterface) *Matcher {
	return &Matcher{Store: store}
}

// FindMatchingExpectation finds an expectation that matches the given gRPC call details.
func (m *Matcher) FindMatchingExpectation(fullMethodName string, headers metadata.MD, reqBodyProto proto.Message) *runtime.GRPCCallExpectation {
	expectations := m.Store.GetExpectations()

	reqBodyJSONBytes := []byte("{}") // Default to empty JSON if reqBodyProto is nil or marshalling fails
	if reqBodyProto != nil {
		var err error
		reqBodyJSONBytes, err = storage.DefaultMarshaler.Marshal(reqBodyProto) // Directly use reqBodyProto
		if err != nil {
			log.Printf("grpcmockruntime: error marshalling request body to JSON for matching call '%s': %v", fullMethodName, err)
			// Proceed with an empty JSON representation of the body on error.
			reqBodyJSONBytes = []byte(`{"error_marshalling_request_body": "true"}`)
		}
	}

	for _, exp := range expectations[fullMethodName] {
		if exp.RequestMatcher == nil { // Match-any if no specific matcher
			log.Printf("grpcmockruntime: Matched (any) expectation for %s", fullMethodName)
			return &exp
		}

		// Match Headers
		headersMatch := true
		if exp.RequestMatcher.Headers != nil {
			for key, pattern := range exp.RequestMatcher.Headers {
				vals := headers.Get(key) // metadata.MD.Get returns a slice of strings
				if len(vals) == 0 {
					headersMatch = false
					break
				}
				headerValueMatched := false
				for _, val := range vals {
					if matchesRegex(pattern, val) {
						headerValueMatched = true
						break
					}
				}
				if !headerValueMatched {
					headersMatch = false
					break
				}
			}
		}
		if !headersMatch {
			log.Printf("grpcmockruntime: Header mismatch for expectation on %s. Expected: %v, Actual: %v", fullMethodName, exp.RequestMatcher.Headers, headers)
			continue // Try next expectation
		}

		// Match Body
		bodyMatch := true
		if exp.RequestMatcher.Body != nil {
			if string(reqBodyJSONBytes) == "{}" && len(exp.RequestMatcher.Body) > 0 {
				bodyMatch = false
			} else if string(reqBodyJSONBytes) == `{"error_marshalling_request_body": "true"}` && len(exp.RequestMatcher.Body) > 0 {
				bodyMatch = false
			} else {
				var actualBodyMap map[string]interface{}
				if err := json.Unmarshal(reqBodyJSONBytes, &actualBodyMap); err != nil {
					log.Printf("grpcmockruntime: error unmarshalling actual request body JSON for matching call '%s': %v. JSON: %s", fullMethodName, err, string(reqBodyJSONBytes))
					bodyMatch = false
				} else {
					if !deepCompare(exp.RequestMatcher.Body, actualBodyMap, true) {
						bodyMatch = false
						log.Printf("grpcmockruntime: Body mismatch for expectation on %s. Expected: %v, Actual (from proto): %v (JSON: %s)", fullMethodName, exp.RequestMatcher.Body, actualBodyMap, string(reqBodyJSONBytes))
					}
				}
			}
		}

		if !bodyMatch {
			continue // Try next expectation
		}

		if headersMatch && bodyMatch {
			log.Printf("grpcmockruntime: Matched expectation for %s (Headers: %v, Body: %v)",
				fullMethodName, headersMatch, bodyMatch)
			return &exp
		}
	}
	log.Printf("grpcmockruntime: No matching expectation found for %s. Checked %d expectations.", fullMethodName, len(expectations[fullMethodName]))
	return nil
}
