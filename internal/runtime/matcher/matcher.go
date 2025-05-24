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

// matchField applies a FieldMatcher to a value.
func matchField(matcher runtime.FieldMatcher, value interface{}) bool {
	if matcher.Equals != nil && !reflect.DeepEqual(matcher.Equals, value) {
		return false
	}
	if matcher.Regex != "" {
		strVal, ok := value.(string)
		if !ok || !matchesRegex(matcher.Regex, strVal) {
			return false
		}
	}
	if matcher.Contains != nil {
		strVal, ok := value.(string)
		substr, ok2 := matcher.Contains.(string)
		if !ok || !ok2 || !contains(strVal, substr) {
			return false
		}
	}
	if matcher.Range != nil {
		floatVal, ok := toFloat64(value)
		if !ok || floatVal < matcher.Range.Min || floatVal > matcher.Range.Max {
			return false
		}
	}
	return true
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (contains(s[1:], substr) || contains(s[:len(s)-1], substr)))))
}

func toFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int, int8, int16, int32, int64:
		return float64(reflect.ValueOf(v).Int()), true
	case uint, uint8, uint16, uint32, uint64:
		return float64(reflect.ValueOf(v).Uint()), true
	default:
		return 0, false
	}
}

// matchHeaders applies HeaderMatcher logic.
func matchHeaders(expected map[string]runtime.HeaderMatcher, actual metadata.MD) bool {
	for key, matcher := range expected {
		vals := actual.Get(key)
		if matcher.Exists != nil {
			exists := len(vals) > 0
			if *matcher.Exists != exists {
				return false
			}
		}
		if matcher.Equals != "" {
			found := false
			for _, v := range vals {
				if v == matcher.Equals {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		if matcher.Regex != "" {
			found := false
			for _, v := range vals {
				if matchesRegex(matcher.Regex, v) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// matchBody applies FieldMatcher logic to the request body.
func matchBody(expected map[string]runtime.FieldMatcher, actual map[string]interface{}) bool {
	for k, matcher := range expected {
		v, ok := actual[k]
		if !ok {
			return false
		}
		if !matchField(matcher, v) {
			return false
		}
	}
	return true
}

// Matcher provides expectation matching using a storeInterface.
type Matcher struct {
	Store       storeInterface
	matchCounts map[string]int // key: expectation hash or index
}

// New creates a new Matcher with the given store.
func New(store storeInterface) *Matcher {
	return &Matcher{Store: store, matchCounts: make(map[string]int)}
}

// FindMatchingExpectation finds an expectation that matches the given gRPC call details.
func (m *Matcher) FindMatchingExpectation(
	fullMethodName string,
	headers metadata.MD,
	reqBodyProto proto.Message,
) *runtime.GRPCCallExpectation {
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

	var actualBodyMap map[string]interface{}
	_ = json.Unmarshal(reqBodyJSONBytes, &actualBodyMap)

	for idx, exp := range expectations[fullMethodName] {
		if exp.RequestMatcher == nil {
			if m.checkTimes(fullMethodName, idx, &exp) {
				m.incrementMatch(fullMethodName, idx)
				return &exp
			}
			continue
		}
		if exp.RequestMatcher.Headers != nil && !matchHeaders(exp.RequestMatcher.Headers, headers) {
			continue
		}
		if exp.RequestMatcher.Body != nil && !matchBody(exp.RequestMatcher.Body, actualBodyMap) {
			continue
		}
		if m.checkTimes(fullMethodName, idx, &exp) {
			m.incrementMatch(fullMethodName, idx)
			return &exp
		}
	}
	return nil
}

// checkTimes checks if the expectation can be matched again based on its Times field.
func (m *Matcher) checkTimes(fullMethod string, idx int, exp *runtime.GRPCCallExpectation) bool {
	key := fmt.Sprintf("%s#%d", fullMethod, idx)
	count := m.matchCounts[key]
	if exp.Times == nil {
		return true
	}
	if exp.Times.Exact > 0 && count >= exp.Times.Exact {
		return false
	}
	if exp.Times.Max > 0 && count >= exp.Times.Max {
		return false
	}
	return true
}

func (m *Matcher) incrementMatch(fullMethod string, idx int) {
	key := fmt.Sprintf("%s#%d", fullMethod, idx)
	m.matchCounts[key]++
}

// GetMatchCounts returns the current match counts for all expectations.
func (m *Matcher) GetMatchCounts() map[string]int {
	return m.matchCounts
}
