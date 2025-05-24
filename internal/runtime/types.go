package runtime

import (
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// FieldMatcher allows for sophisticated field-level matching.
type FieldMatcher struct {
	Equals   interface{}   `json:"equals,omitempty"`
	Regex    string        `json:"regex,omitempty"`
	Contains interface{}   `json:"contains,omitempty"`
	Range    *RangeMatcher `json:"range,omitempty"`
}

type RangeMatcher struct {
	Min float64 `json:"min,omitempty"`
	Max float64 `json:"max,omitempty"`
}

// HeaderMatcher allows for flexible header matching.
type HeaderMatcher struct {
	Exists *bool  `json:"exists,omitempty"`
	Equals string `json:"equals,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

// ExpectationTimes allows specifying how many times an expectation should be matched.
type ExpectationTimes struct {
	Min   int `json:"min,omitempty"`
	Max   int `json:"max,omitempty"`
	Exact int `json:"exact,omitempty"`
}

// StreamMock allows specifying streaming request/response sequences.
type StreamMock struct {
	ExpectedRequests []RequestMatcher `json:"expectedRequests,omitempty"`
	Responses        []MockResponse   `json:"responses,omitempty"`
}

// GRPCCallExpectation defines how a mock should behave.
type GRPCCallExpectation struct {
	FullMethodName string            `json:"fullMethodName"`
	RequestMatcher *RequestMatcher   `json:"requestMatcher,omitempty"`
	Response       *MockResponse     `json:"response,omitempty"`
	Times          *ExpectationTimes `json:"times,omitempty"`
	Stream         *StreamMock       `json:"stream,omitempty"`
}

// RequestMatcher defines the rules to match an incoming gRPC request.
type RequestMatcher struct {
	Headers map[string]HeaderMatcher `json:"headers,omitempty"`
	Body    map[string]FieldMatcher  `json:"body,omitempty"`
}

// MockResponse defines the response to be returned by the mock.
type MockResponse struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Bodies  []json.RawMessage `json:"bodies,omitempty"` // For streaming responses
	Error   *RPCError         `json:"error,omitempty"`
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
