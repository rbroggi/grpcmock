package api

import (
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// GRPCCallExpectation is the DTO for defining an expectation via the HTTP API.
type GRPCCallExpectation struct {
	// ID is a unique identifier for the expectation, usually assigned by the server.
	// Clients can suggest an ID, but the server may override or ignore it.
	ID string `json:"id,omitempty"`

	// FullMethodName defines the gRPC method name (e.g., "/package.Service/Method"). Required.
	FullMethodName string `json:"fullMethodName"`

	// RequestMatcher defines how to match the request.
	RequestMatcher *RequestMatcher `json:"requestMatcher,omitempty"`

	// Response defines the response to be returned for unary calls or
	// as the single response for successfully completed client-streaming calls.
	Response *MockResponse `json:"response,omitempty"`

	// StreamMock defines expectations for streaming calls (server, client, or bidi).
	// If StreamMock is present, 'Response' field might be used as a final response for
	// client-streaming or ignored for server/bidi streaming if sequence is fully defined here.
	StreamMock *StreamMock `json:"streamMock,omitempty"`

	// ExpectedCallTimes defines how many times the expectation should be matched.
	ExpectedCallTimes *ExpectedCallTimes `json:"expectedCallTimes,omitempty"`
}

// RequestMatcher defines how to match an incoming gRPC request (unary or a single stream message).
type RequestMatcher struct {
	// Headers matches against request headers. Key is header name, value is expected value or pattern.
	// This simplified version uses string-to-string for direct value match.
	// For more complex matching (regex, contains), use HeadersMatcher.
	Headers map[string]string `json:"headers,omitempty"`
	// AdvancedHeadersMatcher allows for more sophisticated header matching per field.
	AdvancedHeadersMatcher *HeadersMatcher `json:"advancedHeadersMatcher,omitempty"`

	// BodyMatcher defines how the request body should be matched.
	BodyMatcher *BodyMatcher `json:"bodyMatcher,omitempty"`
}

// HeadersMatcher allows for flexible header matching using FieldMatchers.
type HeadersMatcher struct {
	// Fields is a map of header names to their FieldMatcher.
	Fields map[string]FieldMatcher `json:"fields,omitempty"`
}

// FieldMatcher allows for flexible matching of field values (e.g., for headers).
type FieldMatcher struct {
	Equals   string `json:"equals,omitempty"`   // Exact string match
	Regex    string `json:"regex,omitempty"`    // Regex pattern match
	Contains string `json:"contains,omitempty"` // Substring match
}

// BodyMatcher defines how the request body should be matched.
// Only one of 'Equals' or 'Contains' should be provided.
type BodyMatcher struct {
	// Equals expects a JSON object. The actual request body must exactly match this object.
	Equals interface{} `json:"equals,omitempty"`

	// Contains expects a JSON object. The actual request body must contain this object as a subset.
	Contains interface{} `json:"contains,omitempty"`
}

// MockResponse defines the response to be returned by the mock.
type MockResponse struct {
	// Headers is a map of headers to be returned in the response.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is used for unary responses or individual stream messages.
	// It contains a JSON representation of the protobuf message.
	Body json.RawMessage `json:"body,omitempty"` // Use json.RawMessage to keep it as is from user

	// Error, if set, will cause the mock to return a gRPC error.
	// If Error is set, Body and Headers might be ignored by gRPC framework for error responses.
	Error *RPCError `json:"error,omitempty"`

	// Delay in milliseconds before this response/message is sent.
	Delay int64 `json:"delay,omitempty"`
}

// RPCError defines a gRPC error to be returned.
type RPCError struct {
	Code    codes.Code `json:"code"` // Numeric gRPC status code
	Message string     `json:"message"`
}

// ExpectedCallTimes allows specifying call count expectations.
type ExpectedCallTimes struct {
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// StreamMock defines behavior for streaming RPCs.
type StreamMock struct {
	// For server-streaming or bi-directional streaming: a sequence of responses to send.
	// For client-streaming: this might be empty if 'Response' field in GRPCCallExpectation is used
	// for the final response after client finishes streaming.
	Responses []MockResponse `json:"responses,omitempty"` // Sequence of messages to send from server to client

	// For client-streaming or bi-directional streaming: a sequence of request matchers
	// for messages received from the client.
	ExpectedRequests []RequestMatcher `json:"expectedRequests,omitempty"`
}

// RecordedGRPCCall stores information about an actual call received by the mock (DTO for API).
type RecordedGRPCCall struct {
	ID             string      `json:"id"`
	ExpectationID  string      `json:"expectationId,omitempty"`
	FullMethodName string      `json:"fullMethodName"`
	Type           string      `json:"type"` // "UNARY", "CLIENT_STREAM", etc.
	Headers        metadata.MD `json:"headers"`
	RequestBody    interface{} `json:"requestBody,omitempty"` // map[string]interface{} or string
	// ResponseBody   interface{} `json:"responseBody,omitempty"` // If we record mock's response
	// Error          *RPCError   `json:"error,omitempty"`        // If mock returned an error
	Timestamp int64 `json:"timestamp"`
	Matched   bool  `json:"matched"`
	// StreamMessages []RecordedStreamMessage `json:"streamMessages,omitempty"` // For detailed stream logging
}

// RecordedStreamMessage DTO for API
// type RecordedStreamMessage struct {
//  Direction string      `json:"direction"` // "SENT" (by mock) or "RECEIVED" (by mock)
//  Body      interface{} `json:"body,omitempty"`
//  Error     *RPCError   `json:"error,omitempty"`
//  Timestamp int64       `json:"timestamp"`
// }

// CreateExpectationResponse is the DTO for the response after creating an expectation.
type CreateExpectationResponse struct {
	ID string `json:"id"`
}
