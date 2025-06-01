package core

import "google.golang.org/grpc/codes"

// ExpectationType defines if an expectation is Unary, ClientStream, etc.
type ExpectationType string

const (
	// ExpectationTypeUndefined is the zero value for ExpectationType.
	ExpectationTypeUndefined ExpectationType = ""
	// ExpectationUnary indicates a unary RPC.
	ExpectationUnary ExpectationType = "UNARY"
	// ExpectationClientStream indicates a client-streaming RPC.
	ExpectationClientStream ExpectationType = "CLIENT_STREAM"
	// ExpectationServerStream indicates a server-streaming RPC.
	ExpectationServerStream ExpectationType = "SERVER_STREAM"
	// ExpectationBidiStream indicates a bidirectional-streaming RPC.
	ExpectationBidiStream ExpectationType = "BIDI_STREAM"
)

// Expectation is the core internal representation of an expectation.
type Expectation struct {
	ID                string
	FullMethodName    string
	Type              ExpectationType
	RequestCondition  RequestCondition
	ResponseAction    ResponseAction // For unary and final client-stream response
	StreamActions     []StreamAction // For server-streaming and bidi-streaming sequences
	ExpectedCallTimes *ExpectedCallTimes
}

// RequestCondition defines how an incoming request should be matched.
// For streaming, this might apply to the initial request or individual messages.
type RequestCondition struct {
	HeadersMatcher HeadersMatcher
	BodyMatcher    BodyMatcher // For unary or individual stream messages
}

// HeadersMatcher for internal use.
type HeadersMatcher struct {
	// Map of header names to their FieldMatcher.
	// If nil or empty, header matching is skipped.
	Fields map[string]FieldMatcher
}

// FieldMatcher for internal use, typically for header values.
type FieldMatcher struct {
	Equals   string // Exact match
	Regex    string // Regex match
	Contains string // Substring match
}

// BodyMatchStrategy defines the type of matching to perform on the body.
type BodyMatchStrategy string

const (
	// BodyMatchUndefined indicates no body matching strategy is set (matches any body).
	BodyMatchUndefined BodyMatchStrategy = ""
	// BodyMatchFull means the actual body must be an exact match to ExpectedBody.
	BodyMatchFull BodyMatchStrategy = "FULL_EXACT_MATCH"
	// BodyMatchSubset means the actual body must contain ExpectedBody as a subset.
	BodyMatchSubset BodyMatchStrategy = "SUBSET_MATCH"
)

// BodyMatcher is the internal representation for body matching criteria.
type BodyMatcher struct {
	Strategy BodyMatchStrategy
	// ExpectedBody will be used for either full match or subset match,
	// depending on the Strategy. It's a map representing a parsed JSON object.
	ExpectedBody map[string]interface{}
}

// ResponseAction defines what the mock server should return for a unary call
// or as a single response in a stream.
type ResponseAction struct {
	Headers map[string]string
	Body    map[string]interface{} // For unary response, JSON-like structure
	Error   *RPCError
	Delay   int64 // Optional delay in milliseconds before sending this response
}

// StreamAction defines an action within a stream, which could be expecting a client message
// or sending a server message. Primarily for bi-di, but can simplify server/client streaming too.
// For server streaming, it's a sequence of ResponseActions.
// For client streaming, it might be a sequence of RequestConditions to match incoming messages.
type StreamAction struct {
	// For server-side messages in server-stream or bidi-stream
	Send *ResponseAction
	// For expected client-side messages in client-stream or bidi-stream
	Receive *RequestCondition
	// For bidi, you might have ExpectThenSend { Receive *RequestCondition, Send *ResponseAction }
}

// RPCError defines a gRPC error.
type RPCError struct {
	Code    codes.Code
	Message string
}

// ExpectedCallTimes defines how many times an expectation should be matched.
type ExpectedCallTimes struct {
	Min *int // Minimum number of times. If nil, no minimum.
	Max *int // Maximum number of times. If nil, no maximum.
}

// IsSatisfied checks if the given count satisfies the expected call times.
func (e *ExpectedCallTimes) IsSatisfied(count int) bool {
	if e == nil {
		return true // No expectation means it can be matched any number of times.
	}
	if e.Min != nil && count < *e.Min {
		return false
	}
	if e.Max != nil && count > *e.Max { // Max is an inclusive upper bound
		return false
	}
	return true
}
