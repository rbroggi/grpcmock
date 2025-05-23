package runtime

import (
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// GRPCCallExpectation defines how a mock should behave.
type GRPCCallExpectation struct {
	FullMethodName string          `json:"fullMethodName"`
	RequestMatcher *RequestMatcher `json:"requestMatcher,omitempty"`
	Response       *MockResponse   `json:"response"`
	// TODO: Add fields for verification counts, call order, etc.
	// Times          *ExpectationTimes `json:"times,omitempty"` // Example for future extension
}

// RequestMatcher defines the rules to match an incoming gRPC request.
type RequestMatcher struct {
	Headers map[string]string      `json:"headers,omitempty"` // Key: header name, Value: regex to match header value
	Body    map[string]interface{} `json:"body,omitempty"`    // Fields to match in the request body (after JSON conversion)
	// TODO: Add more sophisticated body matchers (e.g., contains, regex per field, JSONPath)
}

// MockResponse defines the response to be returned by the mock.
type MockResponse struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"` // JSON representation of the protobuf response
	Error   *RPCError         `json:"error,omitempty"`
	// TODO: Add support for streaming responses (e.g., array of Body messages, or a stream control object)
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
