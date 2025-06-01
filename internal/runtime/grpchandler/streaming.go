package grpchandler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"time"

	"github.com/google/uuid" // Make sure to import for uuid.NewString()
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"github.com/rbroggi/grpcmock/internal/runtime/matching"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// StreamHandler handles all types of gRPC streaming calls.
type StreamHandler struct {
	matcherService matcherService
	storageService store
}

// NewStreamHandler creates a new StreamHandler.
func NewStreamHandler(m matcherService, s store) *StreamHandler {
	return &StreamHandler{matcherService: m, storageService: s}
}

// HandleServerStream processes a server-streaming gRPC call.
// The generated template code will iterate through expectation.StreamActions to send messages.
func (h *StreamHandler) HandleServerStream(
	fullMethodName string,
	requestProto proto.Message,
	stream grpc.ServerStream,
) error {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx)
	log.Printf("grpchandler-serverstream: Received call to %s", fullMethodName)

	expectation, _, err := h.matcherService.FindMatchingExpectation(
		ctx,
		fullMethodName,
		incomingMD,
		requestProto,
		core.ExpectationServerStream,
	)

	if err != nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, "", false)
		return status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}
	if expectation == nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, "", false)
		return status.Errorf(codes.Unimplemented, "no matching expectation for server stream %s", fullMethodName)
	}
	// Allow Bidi expectations if they only involve sending (effectively server stream) based on StreamActions
	isCompatibleBidi := expectation.Type == core.ExpectationBidiStream && len(expectation.StreamActions) > 0
	if expectation.Type != core.ExpectationServerStream && !isCompatibleBidi {
		log.Printf("grpchandler-serverstream: Matched expectation %s for %s is not of type SERVER_STREAM or compatible BIDI (type: %s)", expectation.ID, fullMethodName, expectation.Type) //
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID, false)
		return status.Errorf(codes.Internal, "matched expectation is not for server streaming calls")
	}
	if isCompatibleBidi { // If bidi, check if all actions are 'Send'
		for _, action := range expectation.StreamActions {
			if action.Receive != nil {
				log.Printf("grpchandler-serverstream: Matched BIDI expectation %s for %s has 'Receive' actions, not suitable for pure server stream.", expectation.ID, fullMethodName)
				RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID, false)
				return status.Errorf(codes.Internal, "BIDI expectation for server stream has receive actions")
			}
		}
	}

	currentMatchCount, errInc := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if errInc != nil {
		log.Printf("grpchandler-serverstream: Failed to increment match count for exp %s: %v", expectation.ID, errInc)
	}
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID, false)
		return status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times", expectation.ID, fullMethodName)
	}

	RecordSuccessfulCall(ctx, h.storageService, fullMethodName, core.ExpectationServerStream, incomingMD, requestProto, expectation.ID)

	// The template will iterate through expectation.StreamActions to send messages.
	// This handler's job is primarily to find and validate the expectation.
	// Send initial headers if defined in the *first* stream action's Send part.
	if len(expectation.StreamActions) > 0 && expectation.StreamActions[0].Send != nil && len(expectation.StreamActions[0].Send.Headers) > 0 { //
		if err := stream.SendHeader(metadata.New(expectation.StreamActions[0].Send.Headers)); err != nil { //
			log.Printf("grpchandler-serverstream: Failed to send initial headers for %s: %v", fullMethodName, err)
			return status.Errorf(codes.Internal, "failed to send headers: %v", err)
		}
	}
	// The template will loop through stream actions. If any action specifies an error, template will return it.
	// If all actions are processed successfully by template, it returns nil.
	return nil
}

// HandleClientStream processes a client-streaming gRPC call.
func (h *StreamHandler) HandleClientStream( //
	fullMethodName string,
	stream grpc.ServerStream,
	newRequestFunc func() proto.Message,
) (responseBody map[string]interface{}, responseHeaders metadata.MD, err error) {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx)
	log.Printf("grpchandler-clientstream: Received call to %s", fullMethodName)

	expectation, _, err := h.matcherService.FindMatchingExpectation( //
		ctx,
		fullMethodName,
		incomingMD,
		nil, // No initial body for matching the expectation itself, body comes in stream
		core.ExpectationClientStream,
	)

	if err != nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, "", false)
		return nil, nil, status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}
	if expectation == nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, "", false)
		return nil, nil, status.Errorf(codes.Unimplemented, "no matching expectation for client stream %s", fullMethodName)
	}
	// Allow Bidi expectations if they only involve receiving then one final send.
	isCompatibleBidi := expectation.Type == core.ExpectationBidiStream && len(expectation.StreamActions) > 0
	if expectation.Type != core.ExpectationClientStream && !isCompatibleBidi {
		log.Printf("grpchandler-clientstream: Matched expectation %s for %s is not of type CLIENT_STREAM or compatible BIDI (type: %s)", expectation.ID, fullMethodName, expectation.Type) //
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, expectation.ID, false)
		return nil, nil, status.Errorf(codes.Internal, "matched expectation is not for client streaming calls")
	}
	if isCompatibleBidi { // If bidi, check if all actions are 'Receive'
		for _, action := range expectation.StreamActions {
			if action.Send != nil {
				log.Printf("grpchandler-clientstream: Matched BIDI expectation %s for %s has 'Send' actions, not suitable for pure client stream (except final response).", expectation.ID, fullMethodName)
				RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, expectation.ID, false)
				return nil, nil, status.Errorf(codes.Internal, "BIDI expectation for client stream has intermediate send actions")
			}
		}
	}

	currentMatchCount, errInc := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if errInc != nil {
		log.Printf("grpchandler-clientstream: Failed to increment match count for exp %s: %v", expectation.ID, errInc)
	}
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationClientStream, incomingMD, nil, expectation.ID, false)
		return nil, nil, status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times", expectation.ID, fullMethodName)
	}

	expectedRequestIdx := 0 //
	var receivedMessagesForRecording []interface{}

	for {
		req := newRequestFunc()
		errRecv := stream.RecvMsg(req)

		if errRecv == io.EOF {
			log.Printf("grpchandler-clientstream: Client finished streaming for %s", fullMethodName)
			// Check if all expected 'Receive' actions were met
			if len(expectation.StreamActions) > 0 && expectedRequestIdx < len(expectation.StreamActions) {
				for i := expectedRequestIdx; i < len(expectation.StreamActions); i++ {
					if expectation.StreamActions[i].Receive != nil {
						log.Printf("grpchandler-clientstream: Client EOF but expected more messages for %s (exp %s, action %d)", fullMethodName, expectation.ID, i)
						errPrematureEOF := fmt.Errorf("client closed stream prematurely, expected message at index %d", i)
						recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, errPrematureEOF)
						return nil, nil, status.Errorf(codes.InvalidArgument, errPrematureEOF.Error())
					}
				}
			}
			break // Client closed the stream
		}
		if errRecv != nil {
			log.Printf("grpchandler-clientstream: Error receiving from client stream for %s: %v", fullMethodName, errRecv)
			recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, errRecv)
			return nil, nil, status.Errorf(codes.Internal, "error receiving from client stream: %v", errRecv)
		}

		reqMap, _ := ConvertProtoToMap(req) // Error converting individual message is logged by ConvertProtoToMap
		receivedMessagesForRecording = append(receivedMessagesForRecording, reqMap)

		if len(expectation.StreamActions) > 0 && expectedRequestIdx < len(expectation.StreamActions) {
			streamAction := expectation.StreamActions[expectedRequestIdx]
			if streamAction.Receive != nil {
				actualBodyMap, convErr := ConvertProtoToMap(req)
				if convErr != nil {
					log.Printf("grpchandler-clientstream: Error converting received msg to map for matching: %v", convErr)                                 //
					recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, convErr) //
					return nil, nil, status.Errorf(codes.Internal, "error processing received message: %v", convErr)
				}
				if !h.matchesBodyInternal(streamAction.Receive.BodyMatcher, actualBodyMap) { //
					log.Printf("grpchandler-clientstream: Received message %d for %s did not match expectation %s", expectedRequestIdx, fullMethodName, expectation.ID)
					errMismatch := fmt.Errorf("received message at index %d did not match expected pattern", expectedRequestIdx)
					recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, false, errMismatch)
					return nil, nil, status.Errorf(codes.InvalidArgument, errMismatch.Error())
				}
				log.Printf("grpchandler-clientstream: Received message %d for %s matched expectation.", expectedRequestIdx, fullMethodName)
				expectedRequestIdx++
			} else { //
				log.Printf("grpchandler-clientstream: Warning - StreamAction for client stream expected a Receive, but found Send/nil for exp %s, action %d", expectation.ID, expectedRequestIdx)
				// This could be an error if the expectation is strictly for client-side messages.
				// For now, if it's not a Receive, we assume we just keep receiving.
			}
		} else if len(expectation.StreamActions) > 0 && expectedRequestIdx >= len(expectation.StreamActions) {
			log.Printf("grpchandler-clientstream: Client sent more messages than defined in expectation sequence for %s", fullMethodName) //
		}
	}

	finalAction := expectation.ResponseAction //
	if finalAction.Delay > 0 {                //
		time.Sleep(time.Duration(finalAction.Delay) * time.Millisecond)
	}
	if len(finalAction.Headers) > 0 {
		responseHeaders = metadata.New(finalAction.Headers) //
	}
	if finalAction.Error != nil {
		log.Printf("grpchandler-clientstream: Returning error for %s (exp %s) after client stream: %v", fullMethodName, expectation.ID, finalAction.Error.Message)
		recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, true, errors.New(finalAction.Error.Message))
		return nil, responseHeaders, status.Errorf(finalAction.Error.Code, finalAction.Error.Message)
	}

	log.Printf("grpchandler-clientstream: Returning success response for %s (exp %s) after client stream.", fullMethodName, expectation.ID)
	recordClientStreamEnd(ctx, h.storageService, fullMethodName, incomingMD, expectation.ID, receivedMessagesForRecording, true, nil)
	return finalAction.Body, responseHeaders, nil
}

// HandleBidiStream
func (h *StreamHandler) HandleBidiStream( //
	fullMethodName string,
	stream grpc.ServerStream,
	newRequestFunc func() proto.Message,
) error {
	ctx := stream.Context()
	incomingMD, _ := metadata.FromIncomingContext(ctx)
	log.Printf("grpchandler-bidistream: Call to %s", fullMethodName)

	expectation, _, err := h.matcherService.FindMatchingExpectation(
		ctx, fullMethodName, incomingMD, nil, core.ExpectationBidiStream,
	)
	if err != nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, "", false)
		return status.Errorf(codes.Internal, "bidi: error finding expectation: %v", err)
	}
	if expectation == nil {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, "", false)
		return status.Errorf(codes.Unimplemented, "bidi: no matching expectation for %s", fullMethodName)
	}
	if expectation.Type != core.ExpectationBidiStream {
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, expectation.ID, false)
		return status.Errorf(codes.Internal, "bidi: matched expectation %s is not for bidi streams", expectation.ID)
	}

	currentMatchCount, errInc := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if errInc != nil {
		log.Printf("grpchandler-bidistream: Failed to inc match count exp %s: %v", expectation.ID, errInc)
	}
	if expectation.ExpectedCallTimes != nil && !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) { //
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, expectation.ID, false)
		return status.Errorf(codes.ResourceExhausted, "bidi: expectation %s for %s matched max times", expectation.ID, fullMethodName)
	}

	RecordSuccessfulCall(ctx, h.storageService, fullMethodName, core.ExpectationBidiStream, incomingMD, nil, expectation.ID)

	headersSent := false
	var activeClientMessages []interface{}

	for i, action := range expectation.StreamActions { //
		//if action.Delay > 0 {
		//	time.Sleep(time.Duration(action.Delay) * time.Millisecond)
		//}

		if action.Receive != nil {
			log.Printf("grpchandler-bidistream: [%s exp %s action %d] Expecting client message", fullMethodName, expectation.ID, i)
			req := newRequestFunc()
			if errRecv := stream.RecvMsg(req); errRecv != nil { //
				if errors.Is(errRecv, io.EOF) {
					isUnexpectedEOF := false
					for j := i; j < len(expectation.StreamActions); j++ {
						if expectation.StreamActions[j].Receive != nil {
							isUnexpectedEOF = true
							break
						}
					}
					if isUnexpectedEOF {
						log.Printf("grpchandler-bidistream: [%s exp %s] Unexpected EOF at action %d", fullMethodName, expectation.ID, i)
						return status.Errorf(codes.OutOfRange, "client closed stream prematurely at action %d", i)
					}
					log.Printf("grpchandler-bidistream: [%s exp %s] Clean client EOF at action %d", fullMethodName, expectation.ID, i)
					return nil
				}
				log.Printf("grpchandler-bidistream: [%s exp %s action %d] RecvMsg error: %v", fullMethodName, expectation.ID, i, errRecv)
				return status.Errorf(codes.Internal, "bidi: error receiving message: %v", errRecv)
			}
			actualBodyMap, convErr := ConvertProtoToMap(req)
			if convErr != nil {
				log.Printf("grpchandler-bidistream: [%s exp %s action %d] Error converting received message: %v", fullMethodName, expectation.ID, i, convErr)
				return status.Errorf(codes.Internal, "bidi: error converting received message: %v", convErr)
			}
			activeClientMessages = append(activeClientMessages, actualBodyMap)

			if !h.matchesBodyInternal(action.Receive.BodyMatcher, actualBodyMap) { //
				log.Printf("grpchandler-bidistream: [%s exp %s action %d] Received message did not match expectation.", fullMethodName, expectation.ID, i)
				return status.Errorf(codes.InvalidArgument, "bidi: received message at action %d did not match body expectation", i)
			}
			log.Printf("grpchandler-bidistream: [%s exp %s action %d] Matched client message.", fullMethodName, expectation.ID, i)

		} else if action.Send != nil {
			sendAction := action.Send
			log.Printf("grpchandler-bidistream: [%s exp %s action %d] Preparing to send server message.", fullMethodName, expectation.ID, i)
			if !headersSent && len(sendAction.Headers) > 0 {
				if err := stream.SendHeader(metadata.New(sendAction.Headers)); err != nil {
					log.Printf("grpchandler-bidistream: [%s exp %s action %d] Error sending headers: %v", fullMethodName, expectation.ID, i, err)
					return status.Errorf(codes.Internal, "bidi: failed to send headers: %v", err)
				}
				headersSent = true
			}
			if sendAction.Error != nil {
				log.Printf("grpchandler-bidistream: [%s exp %s action %d] Sending error: %v", fullMethodName, expectation.ID, i, sendAction.Error.Message)
				return status.Errorf(sendAction.Error.Code, sendAction.Error.Message)
			}
			// The template is responsible for creating the typed proto and sending it.
			// This handler signals to the template what action.Send.Body to use.
			log.Printf("grpchandler-bidistream: [%s exp %s action %d] Signaling template to send body: %+v (actual send in template)", fullMethodName, expectation.ID, i, sendAction.Body)
		}
	}
	log.Printf("grpchandler-bidistream: [%s exp %s] Finished all stream actions.", fullMethodName, expectation.ID)
	return nil
}

// recordClientStreamEnd is a helper for client stream recording.
// It's called once when the client stream interaction finishes.
func recordClientStreamEnd(
	ctx context.Context,
	store store, // Use the main storage.Store interface
	fullMethodName string,
	md metadata.MD,
	expectationID string,
	receivedMessages []interface{}, // Slice of map[string]interface{}
	overallMatchStatus bool,
	streamError error, // Error from the overall stream processing, if any
) {
	if store == nil {
		log.Println("grpchandler-record: Warning - storage service is nil, cannot record client stream end.")
		return
	}

	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID,
		FullMethodName: fullMethodName,
		Type:           core.ExpectationClientStream,
		Headers:        md.Copy(),
		RequestBody:    receivedMessages, // Store the list of received message maps
		Timestamp:      time.Now().UnixNano(),
		Matched:        overallMatchStatus,
	}
	// If streamError is not nil, we might want to record it in the core.RecordedCall.
	// core.RecordedCall currently doesn't have a general Error field.
	// This might be an enhancement for core.RecordedCall.
	// For now, `Matched` status reflects success/failure.

	if err := store.RecordCall(ctx, call); err != nil { //
		log.Printf("grpchandler-record: Failed to record client stream end for %s: %v", fullMethodName, err)
	} else {
		log.Printf("grpchandler-record: Successfully recorded client stream end for %s, expectation %s, matched: %t", fullMethodName, expectationID, overallMatchStatus)
	}
}

// matchesBodyInternal provides the body matching logic used by stream handlers.
// This is a helper that should ideally live in or be called via the matching.Service.
func (h *StreamHandler) matchesBodyInternal(expected core.BodyMatcher, actualBodyMap map[string]interface{}) bool {
	// This relies on the matcherService field of StreamHandler.
	// To avoid type assertion to the concrete type matching.Service,
	// the matching.Service interface should expose a method like MatchesBody.
	// For now, we are calling the unexported method from matching.Service.
	// This is a temporary workaround during refactoring if matching.Service concrete type is matching.matcherService
	if _, ok := h.matcherService.(*matching.Service); ok { // This is a pointer to the struct from matching package.
		// Replicate logic or call an exported helper from matching package
		// This is a placeholder for calling the actual matching logic, e.g.,
		// return ms.MatchesBodyDetailed(expected, actualBodyMap)
		// For now, replicating the logic as it was in the previous iteration:
		switch expected.Strategy {
		case core.BodyMatchUndefined:
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
			return isSubsetHelper(expected.ExpectedBody, actualBodyMap) // Using the local helper
		default:
			log.Printf("WARN: Unknown body matching strategy in stream handler (matchesBodyInternal): %s", expected.Strategy)
			return false
		}
	}
	log.Printf("WARN: Could not perform body match in stream handler due to matcherService type issue.")
	return false
}

// isSubsetHelper is a local copy of the subset logic for stream handler's internal use.
// This helps avoid direct dependency on unexported functions from matching package or cyclic dependencies.
func isSubsetHelper(subsetCandidate, supersetCandidate map[string]interface{}) bool {
	if subsetCandidate == nil {
		return true
	}
	if supersetCandidate == nil {
		return len(subsetCandidate) == 0
	} // only if subset is also nil/empty

	for key, subValue := range subsetCandidate {
		superValue, ok := supersetCandidate[key]
		if !ok {
			return false
		}

		subMap, subIsMap := subValue.(map[string]interface{})
		superMap, superIsMap := superValue.(map[string]interface{})
		_, subIsSlice := subValue.([]interface{})
		_, superIsSlice := superValue.([]interface{})

		if subIsMap && superIsMap {
			if !isSubsetHelper(subMap, superMap) {
				return false
			}
		} else if subIsSlice && superIsSlice {
			if !reflect.DeepEqual(subValue, superValue) {
				return false
			}
		} else if subIsMap != superIsMap || subIsSlice != superIsSlice { // Type mismatch
			return false
		} else { // Primitives
			if !reflect.DeepEqual(subValue, superValue) {
				return false
			}
		}
	}
	return true
}
