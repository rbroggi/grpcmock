package grpchandler

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"github.com/rbroggi/grpcmock/internal/runtime/matching"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type store interface {
	RecordCall(ctx context.Context, call *core.RecordedCall) error
	IncrementMatchCount(ctx context.Context, expectationID string) (int, error)
}

// UnaryHandler handles unary gRPC calls.
type UnaryHandler struct {
	matcherService matching.Service
	storageService store
}

// NewUnaryHandler creates a new UnaryHandler.
func NewUnaryHandler(m matching.Service, s store) *UnaryHandler {
	return &UnaryHandler{matcherService: m, storageService: s}
}

// Handle processes a unary gRPC call.
// It returns the response body as map[string]interface{}, response headers, and an error.
// The caller (generated server code) is responsible for unmarshalling the map into the concrete proto type.
func (h *UnaryHandler) Handle(
	ctx context.Context,
	fullMethodName string,
	incomingMD metadata.MD,
	requestProto proto.Message,
) (responseBody map[string]interface{}, responseHeaders metadata.MD, err error) {

	log.Printf("grpchandler-unary: Received call to %s", fullMethodName)

	// 1. Attempt to find a matching expectation
	expectation, matchCountSoFar, err := h.matcherService.FindMatchingExpectation(
		ctx,
		fullMethodName,
		incomingMD,
		requestProto,
		core.ExpectationUnary, // Indicate this handler is for unary calls
	)
	if err != nil {
		log.Printf("grpchandler-unary: Error finding matching expectation for %s: %v", fullMethodName, err)
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, "", false)
		return nil, nil, status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}

	if expectation == nil {
		log.Printf("grpchandler-unary: No matching expectation found for %s", fullMethodName)
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, "", false)
		return nil, nil, status.Errorf(codes.Unimplemented, "no matching expectation for method %s", fullMethodName)
	}

	// Check if expectation type is indeed Unary for safety, though FindMatchingExpectation should handle it.
	if expectation.Type != core.ExpectationUnary {
		log.Printf("grpchandler-unary: Matched expectation %s for %s is not of type UNARY (type: %s)", expectation.ID, fullMethodName, expectation.Type)
		recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, expectation.ID, false)
		return nil, nil, status.Errorf(codes.Internal, "matched expectation is not for unary calls")
	}

	// 2. Increment match count and check call limits
	currentMatchCount, err := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if err != nil {
		log.Printf("grpchandler-unary: Failed to increment match count for expectation %s: %v", expectation.ID, err)
		// Continue processing, but this is an internal issue.
	}
	if expectation.ExpectedCallTimes != nil {
		// The count passed to IsSatisfied should be the new count *after* this match.
		if !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) {
			// This typically means we've exceeded Max.
			// We might have already filtered this in FindMatchingExpectation for Max.
			// This re-check is more for complex Min/Max scenarios or if preemptive check wasn't exhaustive.
			log.Printf("grpchandler-unary: Expectation %s for %s call count %d exceeds limits", expectation.ID, fullMethodName, currentMatchCount)
			// Revert increment? Or just fail the call? For now, fail the call.
			// This specific error might be better as Unimplemented or FailedPrecondition if it implies the mock is "used up".
			recordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, expectation.ID, false) // Matched but limit exceeded
			return nil, nil, status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times (%d)", expectation.ID, fullMethodName, currentMatchCount-1)
		}
	}

	// 3. Record the successful call
	recordSuccessfulCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, expectation.ID)

	// 4. Prepare and return response from expectation's ResponseAction
	action := expectation.ResponseAction

	if action.Delay > 0 {
		time.Sleep(time.Duration(action.Delay) * time.Millisecond)
	}

	if len(action.Headers) > 0 {
		responseHeaders = metadata.New(action.Headers)
	}

	if action.Error != nil {
		log.Printf("grpchandler-unary: Returning error for %s (expectation %s): code=%v, msg=%s", fullMethodName, expectation.ID, action.Error.Code, action.Error.Message)
		return nil, responseHeaders, status.Errorf(action.Error.Code, action.Error.Message)
	}

	// The body in core.ResponseAction is map[string]interface{}
	// The generated server.go will unmarshal this into the specific proto type.
	log.Printf("grpchandler-unary: Returning success response for %s (expectation %s)", fullMethodName, expectation.ID)
	return action.Body, responseHeaders, nil
}

func recordSuccessfulCall(ctx context.Context, store store, fullMethod, callType core.ExpectationType, md metadata.MD, reqBodyProto proto.Message, expectationID string) {
	if store == nil {
		return
	}
	reqMap, err := ConvertProtoToMap(reqBodyProto)
	if err != nil {
		log.Printf("grpchandler-record: Failed to convert request proto to map for recording: %v", err)
		// Potentially record with raw proto bytes or a placeholder if conversion fails
	}
	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID,
		FullMethodName: fullMethod,
		Type:           callType,
		Headers:        md.Copy(), // Make a copy of metadata
		RequestBody:    reqMap,
		Timestamp:      time.Now().UnixNano(),
		Matched:        true,
	}
	if err := store.RecordCall(ctx, call); err != nil {
		log.Printf("grpchandler-record: Failed to record successful call: %v", err)
	}
}

func recordFailedCall(ctx context.Context, store store, fullMethod string, callType core.ExpectationType, md metadata.MD, reqBodyProto proto.Message, expectationID string, matchedButFailedLimit bool) {
	if store == nil {
		return
	}
	reqMap, err := ConvertProtoToMap(reqBodyProto)
	if err != nil {
		log.Printf("grpchandler-record: Failed to convert request proto to map for recording failed call: %v", err)
	}
	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID, // May be empty if no expectation found at all
		FullMethodName: fullMethod,
		Type:           callType,
		Headers:        md.Copy(),
		RequestBody:    reqMap,
		Timestamp:      time.Now().UnixNano(),
		Matched:        matchedButFailedLimit, // True if it matched an expectation but failed due to limits/internal error
	}
	if err := store.RecordCall(ctx, call); err != nil {
		log.Printf("grpchandler-record: Failed to record failed/unmatched call: %v", err)
	}
}
