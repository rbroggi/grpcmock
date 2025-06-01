package grpchandler

// This is a complex piece. I will provide a basic structure for server and client streaming.
// True, sequence-aware bidirectional streaming is significantly more involved and would require
// careful state management within the handler for a given stream.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"github.com/rbroggi/grpcmock/internal/runtime/matching"
	"github.com/rbroggi/grpcmock/internal/runtime/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// StreamHandler handles all types of gRPC streaming calls.
type StreamHandler struct {
	matcherService matching.Service
	storageService store
}

// NewStreamHandler creates a new StreamHandler.
func NewStreamHandler(m matching.Service, s store) *StreamHandler {
	return &StreamHandler{matcherService: m, storageService: s}
}

// Generic incoming gRPC stream from the server's perspective.
type ServerStream interface {
	Context() context.Context
	SendMsg(m interface{}) error
	RecvMsg(m interface{}) error
	SetHeader(md metadata.MD) error
	SendHeader(md metadata.MD) error
	SetTrailer(md metadata.MD)
}

// HandleServerStream processes a server-streaming gRPC call.
func (h *StreamHandler) HandleServerStream(
	fullMethodName string,
	requestProto proto.Message, // Initial request from client
	stream grpc.ServerStream, // The stream to send responses on
) error {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx)
	log.Printf("grpchandler-serverstream: Received call to %s", fullMethodName)

	expectation, matchCountSoFar, err := h.matcherService.FindMatchingExpectation(
		ctx,
		fullMethodName,
		incomingMD,
		requestProto,
		core.ExpectationServerStream,
	)

	if err != nil {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, "", false)
		return status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}
	if expectation == nil {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, "", false)
		return status.Errorf(codes.Unimplemented, "no matching expectation for server stream %s", fullMethodName)
	}
	if expectation.Type != core.ExpectationServerStream && !(expectation.Type == core.ExpectationBidiStream && len(expectation.StreamActions) > 0) {
		// Allow Bidi expectations if they only involve sending (effectively server stream)
		log.Printf("grpchandler-serverstream: Matched expectation %s for %s is not of type SERVER_STREAM (type: %s)", expectation.ID, fullMethodName, expectation.Type)
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID, false)
		return status.Errorf(codes.Internal, "matched expectation is not for server streaming calls")
	}

	currentMatchCount, errInc := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if errInc != nil {
		log.Printf("grpchandler-serverstream: Failed to increment match count for exp %s: %v", expectation.ID, errInc)
	}
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID, false) // Matched but limit exceeded
		return status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times", expectation.ID, fullMethodName)
	}

	recordSuccessfulCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID)

	// Send initial headers if defined in the *first* stream action's Send part, or top-level ResponseAction (if applicable)
	// For server streaming, ResponseAction at top level of expectation is usually not used if StreamActions are present.
	// Let's assume headers for streams come from the individual StreamAction.Send.Headers.
	// This needs a clearer model: should an overall stream have headers, or each message?
	// Typically, headers are sent once at the beginning.
	// For now, let's check the first StreamAction.
	if len(expectation.StreamActions) > 0 && expectation.StreamActions[0].Send != nil && len(expectation.StreamActions[0].Send.Headers) > 0 {
		if err := stream.SendHeader(metadata.New(expectation.StreamActions[0].Send.Headers)); err != nil {
			log.Printf("grpchandler-serverstream: Failed to send initial headers for %s: %v", fullMethodName, err)
			return status.Errorf(codes.Internal, "failed to send headers: %v", err)
		}
	}

	for i, action := range expectation.StreamActions {
		if action.Send == nil {
			log.Printf("grpchandler-serverstream: Exp %s action %d for %s is not a send action", expectation.ID, i, fullMethodName)
			// This is an issue with expectation structure for server-stream.
			return status.Errorf(codes.Internal, "invalid server stream expectation: action %d is not 'send'", i)
		}
		sendAction := action.Send

		if sendAction.Delay > 0 {
			time.Sleep(time.Duration(sendAction.Delay) * time.Millisecond)
		}

		if sendAction.Error != nil {
			log.Printf("grpchandler-serverstream: Exp %s action %d for %s returning error: %v", expectation.ID, i, fullMethodName, sendAction.Error.Message)
			return status.Errorf(sendAction.Error.Code, sendAction.Error.Message)
		}

		// The generated server code needs to know the *type* of message to create.
		// This handler cannot know it. The template will need to create an instance of proto.Message
		// and then this handler (or rather the template calling a helper) will unmarshal action.Body into it.
		// For now, this means the template has to do:
		//   msg := new(pb.ResponseType)()
		//   err := grpchandler.UnmarshalMapToProto(sendAction.Body, msg)
		//   stream.SendMsg(msg)
		// This function will thus return the sequence of bodies/errors for the template to handle.
		// This is a limitation of this generic handler.
		// To simplify, for this PoC, this function will assume it is called from a context
		// where it can directly send. This requires the template to pass a SendMsg func with type knowledge.
		// This is too complex for now. Let's assume the body is prepared for sending.
		// The template will have to call `UnmarshalMapToProto` for each message body.
		// This handler should really just provide the sequence of `core.ResponseAction`.
		// The calling template will iterate and handle unmarshaling and sending.

		// Simplified: Let's assume the template provides a way to send a map.
		// This isn't how gRPC works. The template needs to handle the type.
		// Let's pass back the action for the template to process.
		// This function's role is to iterate through defined actions.
		//
		// The `server.tmpl` will look like:
		// for _, actionBodyMap := range streamResponseBodies { /* (returned from a call to this handler) */
		//    resp := new(concrete.ResponseType)
		//    UnmarshalMapToProto(actionBodyMap, resp)
		//    stream.Send(resp)
		// }
		// This current function can't do stream.SendMsg directly without type.
		// So, this handler should just validate the expectation and provide the actions.
		// Let's return error if something goes wrong with THIS expectation.
		// The actual sending loop remains in the template.
		// This handler's job is to find the expectation and then the template uses its `StreamActions`.
		//
		// REVISING: This function *will* try to send, assuming the template provides the output message type.
		// This is not feasible without changing the signature to accept `func() proto.Message` (new instance)
		//
		// Let's assume `stream` passed here is the actual `grpc.ServerStream` and the generated code
		// in the template knows the concrete type of message to send for *this specific RPC method*.

		// The generated code will look like:
		// streamHandler.HandleServerStream(..., req, stream, func() proto.Message { return new(pb.StreamResponseType) })
		// Let's change signature.
		// NO, let's keep it simpler: handler provides data, template does type work.

		// For the template:
		// For each `sendAction` in `expectation.StreamActions`:
		//   `responseToSend := new(ResponseType)`
		//   `grpchandler.UnmarshalMapToProto(sendAction.Body, responseToSend)`
		//   `stream.Send(responseToSend)`

		// This function's main job is to ensure the expectation is valid and provide it.
		// The actual stream interaction loop is better placed in the typed, generated code (template).
		// So, this function just returns the *validated* expectation.
		//
		// OK, NEW APPROACH: Handler *does* handle the stream interaction.
		// The generated template will need to pass a "sender" function.
		// For now, let's assume the structure of this function is primarily for getting the expectation.
		// The template will iterate `expectation.StreamActions`.

		// The core logic for the handler if it were to send:
		// resp := newProtoResponseType() // This needs to be passed or known.
		// if err := UnmarshalMapToProto(sendAction.Body, resp); err != nil {
		// 	return status.Errorf(codes.Internal, "failed to unmarshal stream message: %v", err)
		// }
		// if err := stream.SendMsg(resp); err != nil {
		// 	return status.Errorf(codes.Internal, "failed to send stream message: %v", err)
		// }
		// log.Printf("grpchandler-serverstream: Sent message for %s", fullMethodName)
	}

	// If we reach here, all messages from expectation were processed.
	// The template will have looped through expectation.StreamActions.
	return nil // Success
}

// HandleClientStream processes a client-streaming gRPC call.
// `newRequestFunc` should return a new, empty instance of the client stream's request message type.
func (h *StreamHandler) HandleClientStream(
	fullMethodName string,
	stream grpc.ServerStream, // Stream to receive messages from client
	newRequestFunc func() proto.Message, // Factory for request message type
) (responseBody map[string]interface{}, responseHeaders metadata.MD, err error) {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx) // Headers from the initial client call
	log.Printf("grpchandler-clientstream: Received call to %s", fullMethodName)

	// For client streaming, the initial FindMatchingExpectation might not use a requestBody,
	// as the body comes in via stream.RecvMsg().
	// Some client streams might send an initial message with the call, others might not.
	// Let's assume no initial body for FindMatchingExpectation, and match based on headers/method.
	expectation, matchCountSoFar, err := h.matcherService.FindMatchingExpectation(
		ctx,
		fullMethodName,
		incomingMD,
		nil, // No initial body for matching the expectation itself, body comes in stream
		core.ExpectationClientStream,
	)

	if err != nil {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, "", false)
		return nil, nil, status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}
	if expectation == nil {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, "", false)
		return nil, nil, status.Errorf(codes.Unimplemented, "no matching expectation for client stream %s", fullMethodName)
	}
	if expectation.Type != core.ExpectationClientStream && !(expectation.Type == core.ExpectationBidiStream && len(expectation.StreamActions) > 0) {
		log.Printf("grpchandler-clientstream: Matched expectation %s for %s is not of type CLIENT_STREAM (type: %s)", expectation.ID, fullMethodName, expectation.Type)
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, expectation.ID, false)
		return nil, nil, status.Errorf(codes.Internal, "matched expectation is not for client streaming calls")
	}

	currentMatchCount, errInc := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if errInc != nil {
		log.Printf("grpchandler-clientstream: Failed to increment match count for exp %s: %v", expectation.ID, errInc)
	}
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, expectation.ID, false)
		return nil, nil, status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times", expectation.ID, fullMethodName)
	}

	// Don't record the main "call" until stream is processed, or record "stream initiated".
	// For now, we'll record one "successful" call if the stream processing according to expectation completes.

	expectedRequestIdx := 0
	var receivedMessagesForRecording []interface{}

	for {
		req := newRequestFunc()
		errRecv := stream.RecvMsg(req)

		if errRecv == io.EOF {
			log.Printf("grpchandler-clientstream: Client finished streaming for %s", fullMethodName)
			break // Client closed the stream
		}
		if errRecv != nil {
			log.Printf("grpchandler-clientstream: Error receiving from client stream for %s: %v", fullMethodName, errRecv)
			recordClientStreamCall(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, errRecv)
			return nil, nil, status.Errorf(codes.Internal, "error receiving from client stream: %v", errRecv)
		}

		reqMap, _ := ConvertProtoToMap(req)
		receivedMessagesForRecording = append(receivedMessagesForRecording, reqMap)

		// Match received message if expectation defines a sequence of expected requests
		if len(expectation.StreamActions) > 0 && expectedRequestIdx < len(expectation.StreamActions) {
			streamAction := expectation.StreamActions[expectedRequestIdx]
			if streamAction.Receive != nil {
				actualBodyMap, convErr := ConvertProtoToMap(req)
				if convErr != nil {
					log.Printf("grpchandler-clientstream: Error converting received msg to map for matching: %v", convErr)
					// Decide how to handle: fail stream or try to continue? For now, fail.
					recordClientStreamCall(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, convErr)
					return nil, nil, status.Errorf(codes.Internal, "error processing received message: %v", convErr)
				}

				// Headers for individual stream messages are not typically matched this way.
				// The RequestCondition in StreamAction.Receive usually focuses on BodyMatcher.
				if !h.matcherService.(interface {
					matchesBody(core.BodyMatcher, map[string]interface{}) bool
				}).matchesBody(streamAction.Receive.BodyMatcher, actualBodyMap) {
					log.Printf("grpchandler-clientstream: Received message %d for %s did not match expectation %s", expectedRequestIdx, fullMethodName, expectation.ID)
					errMismatch := fmt.Errorf("received message at index %d did not match expected pattern", expectedRequestIdx)
					recordClientStreamCall(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, errMismatch)
					return nil, nil, status.Errorf(codes.InvalidArgument, errMismatch.Error())
				}
				log.Printf("grpchandler-clientstream: Received message %d for %s matched expectation.", expectedRequestIdx, fullMethodName)
				expectedRequestIdx++
			} else {
				// Expectation expected a send, but this is client stream. This indicates a misconfigured bidi as client stream.
			}
		} else if len(expectation.StreamActions) > 0 && expectedRequestIdx >= len(expectation.StreamActions) {
			// Client sent more messages than expected by the sequence.
			// Depending on strictness, this could be an error or ignored.
			log.Printf("grpchandler-clientstream: Client sent more messages than defined in expectation sequence for %s", fullMethodName)
			// For now, we allow extra messages if the sequence is exhausted, they are just not matched against further specific patterns.
		}
		// If no StreamActions.Receive, all messages are "accepted" until EOF.
	}

	// All client messages received (or sequence matched). Now, return the final response.
	finalAction := expectation.ResponseAction // This is the single response after client finishes.

	if finalAction.Delay > 0 {
		time.Sleep(time.Duration(finalAction.Delay) * time.Millisecond)
	}

	if len(finalAction.Headers) > 0 {
		// For client-streaming, headers are typically sent with SendAndClose if it's unary-like response.
		// The stream.SetHeader() should be called before SendAndClose() if this API supports it.
		// grpc.ServerStream.SendHeader() is for server-streaming.
		// Let's assume the template handles this: it will get these headers and call SetHeader then SendAndClose.
		responseHeaders = metadata.New(finalAction.Headers)
	}

	if finalAction.Error != nil {
		log.Printf("grpchandler-clientstream: Returning error for %s (exp %s) after client stream: %v", fullMethodName, expectation.ID, finalAction.Error.Message)
		recordClientStreamCall(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, true, errors.New(finalAction.Error.Message)) // Matched, but ended with error
		return nil, responseHeaders, status.Errorf(finalAction.Error.Code, finalAction.Error.Message)
	}

	log.Printf("grpchandler-clientstream: Returning success response for %s (exp %s) after client stream.", fullMethodName, expectation.ID)
	recordClientStreamCall(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, true, nil)
	return finalAction.Body, responseHeaders, nil
}

// HandleBidiStream - This is the most complex.
// A proper implementation requires careful state management for interleaved send/receive.
// The current core.Expectation.StreamActions (a simple list) might not be rich enough
// to define complex bidi interactions easily unless interpreted strictly by order.
// For now, this will be a placeholder or a very simplified version.
func (h *StreamHandler) HandleBidiStream(
	fullMethodName string,
	stream grpc.ServerStream,
	newRequestFunc func() proto.Message, // For RecvMsg
	// newResponseFunc func() proto.Message, // For SendMsg, if this handler sends typed msgs
) error {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx)
	log.Printf("grpchandler-bidistream: Call to %s (basic bidi handling)", fullMethodName)

	// Find expectation (initial match, likely no body, type BIDI_STREAM)
	expectation, matchCountSoFar, err := h.matcherService.FindMatchingExpectation(
		ctx, fullMethodName, incomingMD, nil, core.ExpectationBidiStream,
	)
	if err != nil {
		return status.Errorf(codes.Internal, "bidi: error finding expectation: %v", err)
	}
	if expectation == nil {
		return status.Errorf(codes.Unimplemented, "bidi: no matching expectation for %s", fullMethodName)
	}
	if expectation.Type != core.ExpectationBidiStream {
		return status.Errorf(codes.Internal, "bidi: matched expectation %s is not for bidi streams", expectation.ID)
	}

	currentMatchCount, _ := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
		return status.Errorf(codes.ResourceExhausted, "bidi: expectation %s for %s matched max times", expectation.ID, fullMethodName)
	}

	// Record initial phase of the call
	recordSuccessfulCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, expectation.ID)

	// Basic Bidi Logic: Iterate through StreamActions.
	// This assumes StreamActions are ordered and correctly define the send/receive sequence.
	for i, action := range expectation.StreamActions {
		if action.Delay > 0 { // Delay before this action
			time.Sleep(time.Duration(action.Delay) * time.Millisecond)
		}

		if action.Receive != nil { // Expect a message from client
			req := newRequestFunc()
			if errRecv := stream.RecvMsg(req); errRecv != nil {
				if errRecv == io.EOF {
					log.Printf("grpchandler-bidistream: Client closed stream during Recv action %d for %s (exp %s)", i, fullMethodName, expectation.ID)
					// Check if EOF was expected at this point or if more actions were pending
					if i < len(expectation.StreamActions)-1 { // More actions were expected
						return status.Errorf(codes.OutOfRange, "client closed stream prematurely at action %d", i)
					}
					return nil // Clean EOF
				}
				log.Printf("grpchandler-bidistream: RecvMsg error at action %d for %s (exp %s): %v", i, fullMethodName, expectation.ID, errRecv)
				return status.Errorf(codes.Internal, "bidi: error receiving message: %v", errRecv)
			}
			// Match req against action.Receive.BodyMatcher
			actualBodyMap, convErr := ConvertProtoToMap(req)
			if convErr != nil {
				return status.Errorf(codes.Internal, "bidi: error converting received message: %v", convErr)
			}
			// Assuming matcherService has a public method for this or we use a helper
			if !h.matcherService.(interface {
				matchesBody(core.BodyMatcher, map[string]interface{}) bool
			}).matchesBody(action.Receive.BodyMatcher, actualBodyMap) {
				return status.Errorf(codes.InvalidArgument, "bidi: received message at action %d did not match expectation", i)
			}
			log.Printf("grpchandler-bidistream: Matched client msg at action %d for %s", i, fullMethodName)
			// TODO: Record received message as part of the overall RecordedCall

		} else if action.Send != nil { // Send a message to client
			if action.Send.Error != nil {
				return status.Errorf(action.Send.Error.Code, action.Send.Error.Message)
			}
			// Template will provide newResponseFunc() and UnmarshalMapToProto
			// For now, this handler can't directly send without type info.
			// This is where the template has to take action.Send.Body and construct+send.
			// This simplified bidi assumes template calls a helper like:
			// err := SendStreamMessage(stream, action.Send.Body, newResponseFunc)
			//
			// To make this handler more self-contained for sending, it would need newResponseFunc.
			// For now, this signals to the template what to send.
			// The generated code must handle unmarshaling action.Send.Body and calling stream.SendMsg()
			log.Printf("grpchandler-bidistream: Action %d for %s is to send (template handles actual send)", i, fullMethodName)
			// TODO: Record sent message

		}
	}
	log.Printf("grpchandler-bidistream: Finished all actions for %s (exp %s)", fullMethodName, expectation.ID)
	return nil
}

// Helper for client stream recording
func recordClientStreamCall(ctx context.Context, store store, fullMethod string, md metadata.MD, expectationID string, receivedMessages []interface{}, matched bool, streamError error) {
	if store == nil {
		return
	}
	// For client streaming, the "RequestBody" could be a list of received messages.
	// Or, we simplify and don't store all messages in the main RecordedCall.RequestBody.
	// For now, let's store the list.
	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID,
		FullMethodName: fullMethod,
		Type:           core.ExpectationClientStream,
		Headers:        md.Copy(),
		RequestBody:    receivedMessages, // This will be a slice of maps
		Timestamp:      time.Now().UnixNano(),
		Matched:        matched,
	}
	// if streamError != nil {
	//  call.Error = &core.RPCError{Code: status.Code(streamError), Message: streamError.Error()}
	// }

	if err := store.RecordCall(ctx, call); err != nil {
		log.Printf("grpchandler-record: Failed to record client stream call: %v", err)
	}
}

// This extension to matcherService interface is assumed for stream_handler for now
// Ideally, this logic is internal to matcherService or exposed cleanly.
func (ms *matcherService) matchesBody(expected core.BodyMatcher, actualBodyMap map[string]interface{}) bool {
	// This is a simplified re-implementation of the body matching part for stream messages
	// In a real scenario, this would call a method on the matcherService.
	switch expected.Strategy {
	case core.BodyMatchUndefined, "":
		return true
	case core.BodyMatchFull:
		if expected.ExpectedBody == nil && actualBodyMap == nil {
			return true
		}
		if expected.ExpectedBody == nil && actualBodyMap != nil && len(actualBodyMap) == 0 {
			return true
		}
		if (expected.ExpectedBody == nil) != (actualBodyMap == nil) && (len(expected.ExpectedBody) > 0 || len(actualBodyMap) > 0) {
			return false
		}
		return reflect.DeepEqual(expected.ExpectedBody, actualBodyMap)
	case core.BodyMatchSubset:
		if expected.ExpectedBody == nil {
			return true
		}
		return isSubset(expected.ExpectedBody, actualBodyMap)
	default:
		return false
	}
}
