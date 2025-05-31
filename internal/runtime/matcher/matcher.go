package matcher

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"

	"github.com/rbroggi/grpcmock/internal/runtime"
	"github.com/rbroggi/grpcmock/internal/runtime/storage"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// storeInterface defines the methods for expectation and call storage.
type storeInterface interface {
	ListExpectations(options runtime.ListExpectationsOptions) []runtime.GRPCCallExpectation
	IncrementMatch(id string)
	GetMatches(id string) int
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

// matchField applies a BodyMatcher to a value.
func matchField(matcher runtime.BodyMatcher, value interface{}) bool {
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

// matchHeaders applies HeadersMatcher logic.
func matchHeaders(expected *runtime.HeadersMatcher, actual metadata.MD) bool {
	if expected == nil {
		return true
	}
	for key, fieldMatcher := range expected.HeadersFieldsMatchers {
		vals := actual.Get(key)
		matched := false
		for _, v := range vals {
			if fieldMatcher.Match(v) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// matchBody applies BodyMatcher logic to the request body.
func matchBody(expected *runtime.BodyMatcher, actual map[string]interface{}) bool {
	if expected == nil {
		return true
	}
	// For now, only support Equals and Contains for the whole body as string
	if expected.Equals != nil {
		return reflect.DeepEqual(expected.Equals, actual)
	}
	if expected.Contains != nil {
		// Only works if both are strings
		bodyStr, ok1 := actual["body"].(string)
		containsStr, ok2 := expected.Contains.(string)
		return ok1 && ok2 && strings.Contains(bodyStr, containsStr)
	}
	return true
}

func matchStrict(actualReqJSON, expected []byte) (bool, error) {
	var actualReq interface{}
	if err := json.Unmarshal(actualReqJSON, &actualReq); err != nil {
		return false, fmt.Errorf("invalid actual request json: %v", err)
	}
	var expectedReq interface{}
	if err := json.Unmarshal(expected, &expectedReq); err != nil {
		return false, fmt.Errorf("invalid expected request json: %v", err)
	}
	return reflect.DeepEqual(expectedReq, actualReq), nil
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
// It returns the matching expectation or nil if none found (with no error).
// If an error occurs during matching, it returns nil and the error.
func (m *Matcher) FindMatchingExpectation(
	fullMethodName string,
	headers metadata.MD,
	reqBodyProto proto.Message,
) (*runtime.GRPCCallExpectation, error) {
	expectations := m.Store.ListExpectations(runtime.ListExpectationsOptions{FullMethodName: fullMethodName})
	if len(expectations) == 0 {
		return nil, nil
	}

	var err error
	reqBodyJSONBytes, err := storage.DefaultMarshaler.Marshal(reqBodyProto)
	if err != nil {
		return nil, err
	}

	var actualBodyMap map[string]interface{}
	if err = json.Unmarshal(reqBodyJSONBytes, &actualBodyMap); err != nil {
		log.Printf("grpcmockruntime: error unmarshalling request body to JSON: %v", err)
		return nil, err
	}

	for _, exp := range expectations {
		matcher := exp.RequestMatcher
		if matcher == nil ||
			(matcher.HeadersMatcher == nil || matchHeaders(matcher.HeadersMatcher, headers)) &&
				(matcher.BodyMatcher == nil || matchBody(matcher.BodyMatcher, actualBodyMap)) {
			m.Store.IncrementMatch(exp.ID)
			return &exp, nil
		}
	}

	if b, err := json.MarshalIndent(expectations, "", "  "); err == nil {
		log.Printf("grpcmockruntime: no matching expectation found. Unmatched expectations for method %s: \n%s", fullMethodName, string(b))
	} else {
		log.Printf("grpcmockruntime: failed to marshal unmatched expectations: %v", err)
	}
	return nil, nil
}

func (m *Matcher) VerifyExpectation(id string) (bool, error) {
	expectations := m.Store.ListExpectations(runtime.ListExpectationsOptions{FullMethodName: id})
	if len(expectations) == 0 {
		return false, runtime.ErrNotFound
	}
	return expectations[0].ExpectedCalledTimes.IsSatisfied(m.Store.GetMatches(id)), nil
}

func (m *Matcher) VerifyAll() bool {
	expectations := m.Store.ListExpectations(runtime.ListExpectationsOptions{})
	for _, exp := range expectations {
		if !exp.ExpectedCalledTimes.IsSatisfied(m.Store.GetMatches(exp.ID)) {
			return false
		}
	}
	return true
}
