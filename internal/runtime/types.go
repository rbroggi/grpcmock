package runtime

import (
	"encoding/json"
	"errors"
	"log"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

var (
	ErrNotFound = errors.New("expectation not found")
)

// BodyMatcher allows for sophisticated field-level matching.
type BodyMatcher struct {
	Equals   interface{} `json:"equals,omitempty"`
	Contains interface{} `json:"contains,omitempty"`
}

// HeadersMatcher allows for flexible header matching.
type HeadersMatcher struct {
	// HeadersFieldsMatchers is a map of header names to their matchers.
	HeadersFieldsMatchers map[string]FieldMatcher `json:"headers,omitempty"`
}

// FieldMatcher allows for flexible matching of field values. if none of the
// fields are set, it will match any value (equivalent of requiring existence of
// the field).
type FieldMatcher struct {
	// Equals is a string value that must match exactly.
	Equals string `json:"equals,omitempty"`
	// Regex is a regex pattern that must match.
	Regex string `json:"regex,omitempty"`
	// Contains is a string that must be contained within the value.
	Contains string `json:"contains,omitempty"`
}

func (f FieldMatcher) Match(value string) bool {
	if f.Equals != "" && f.Equals != value {
		return false
	}
	if f.Regex != "" && !matchesRegex(f.Regex, value) {
		return false
	}
	if f.Contains != "" && !strings.Contains(value, f.Contains) {
		return false
	}
	return true
}

func matchesRegex(regex string, value string) bool {
	re, err := regexp.Compile(regex)
	if err != nil {
		log.Printf("grpcmockruntime: regex error matching pattern '%s' with value '%s': %v", regex, value, err)
		return false
	}
	return re.MatchString(value)
}

// ExpectedCalledTimes allows specifying how many times an expectation should be matched.
// It can specify minimum or maximum number of times. If both are set to the same value
// it means the expectation should be matched exactly that many times. If no value is set,
// it means the expectation can be matched any number of times and verification will pass
// regardless of how many times it was matched (including zero times).
type ExpectedCalledTimes struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

func (e *ExpectedCalledTimes) IsSatisfied(count int) bool {
	if e == nil {
		return true // No expectation means it can be matched any number of times
	}
	if e.Min != nil && count < *e.Min {
		return false
	}
	if e.Max != nil && count > *e.Max {
		return false
	}
	return true
}

// StreamMock allows specifying streaming request/response sequences. It can be
// used to define a sequence of expected requests and their corresponding
// responses. For client streaming, the expected requests are matched in order.
// For server streaming, the expected requests are matched in order and the
// responses are sent in order.
type StreamMock struct {
	ExpectedRequests []RequestMatcher `json:"expectedRequests,omitempty"`
	Responses        []MockResponse   `json:"responses,omitempty"`
}

// GRPCCallExpectation defines how a mock should behave.
type GRPCCallExpectation struct {
	// ID is a unique identifier for the expectation that will be returned in the response.
	ID string `json:"id"` // Unique identifier for the expectation
	// FullMethodName defines the gRPC method name (e.g., "/package.Service/Method").
	FullMethodName string `json:"fullMethodName"`
	// RequestMatcher defines how to match the request for unary calls.
	RequestMatcher *RequestMatcher `json:"requestMatcher,omitempty"`
	// Response defines the response to be returned for unary calls.
	Response *MockResponse `json:"response,omitempty"`
	// ExpectedCalledTimes defines how many times the expectation should be matched for unary calls.
	ExpectedCalledTimes *ExpectedCalledTimes `json:"expectedCalledTimes,omitempty"`
	// RequestMatcher defines how to match the request for client streaming calls.
	StreamMock *StreamMock `json:"streamMock,omitempty"`
}

// RequestMatcher defines how to match the request for unary calls.
type RequestMatcher struct {
	HeadersMatcher *HeadersMatcher `json:"headersMatcher,omitempty"`
	BodyMatcher    *BodyMatcher    `json:"bodyMatcher,omitempty"`
}

// MockResponse defines the response to be returned by the mock.
type MockResponse struct {
	// Headers is a map of headers to be returned in the response.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is used for unary responses. It contains a JSON-encoded protobuf message.
	Body json.RawMessage `json:"body,omitempty"`
	// Error is used for unary responses. It contains a gRPC error code and message.
	Error *RPCError `json:"error,omitempty"`
}

// RPCError defines a gRPC error to be returned.
type RPCError struct {
	Code    codes.Code `json:"code"`
	Message string     `json:"message"`
}

// RecordedGRPCCall stores information about an actual call received by the mock.
type RecordedGRPCCall struct {
	FullMethodName string          `json:"fullMethodName"`
	Headers        metadata.MD     `json:"headers"`   // Store as metadata.MD for easier access
	Body           json.RawMessage `json:"body"`      // JSON representation of the protobuf request
	Timestamp      int64           `json:"timestamp"` // Unix nano timestamp
}

// ListExpectationsOptions allows filtering expectations.
type ListExpectationsOptions struct {
	FullMethodName string
}
