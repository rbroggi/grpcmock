package core

import "google.golang.org/grpc/metadata"

// RecordedCall represents a call received by the mock server.
type RecordedCall struct {
	ID             string          // Unique ID for this recorded call
	ExpectationID  string          // ID of the expectation that matched this call (if any)
	FullMethodName string          // Full gRPC method name (e.g., "/package.Service/Method")
	Type           ExpectationType // Type of call (UNARY, CLIENT_STREAM, etc.)
	Headers        metadata.MD     // Incoming request headers
	Timestamp      int64           // Unix nano timestamp of when the call was received

	// For unary calls or the initial request of a stream that carries a body.
	// For client streaming, this might be a list of bodies if we record each message.
	RequestBody interface{} // map[string]interface{} representation of the request body

	// For streaming, we might want to record the sequence of messages.
	// ReceivedMessages []StreamMessage // For client->server messages in a stream
	// SentMessages     []StreamMessage // For server->client messages sent by the mock in a stream
	// Error            *RPCError       // If the handler returned an error for this call

	// Matched indicates if this call was successfully matched to an expectation.
	Matched bool
}

// StreamMessage could represent a single message in a stream.
// type StreamMessage struct {
// 	Body      map[string]interface{}
// 	Timestamp int64
// 	Error     error // If there was an error receiving/sending this specific message
// }
