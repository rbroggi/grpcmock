package grpchandler

import (
	"context"
	"log"
	"time"

	"github.com/rbroggi/grpcmock/internal/runtime/core"
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
	matcherService matcherService
	storageService store
}

// NewUnaryHandler creates a new UnaryHandler.
func NewUnaryHandler(m matcherService, s store) *UnaryHandler { // [cite: 369]
	return &UnaryHandler{matcherService: m, storageService: s}
}

// Handle processes a unary gRPC call.
func (h *UnaryHandler) Handle( // [cite: 372]
	ctx context.Context,
	fullMethodName string,
	incomingMD metadata.MD,
	requestProto proto.Message,
) (responseBody map[string]interface{}, responseHeaders metadata.MD, err error) {

	log.Printf("grpchandler-unary: Received call to %s", fullMethodName)

	expectation, _, err := h.matcherService.FindMatchingExpectation(
		ctx,
		fullMethodName,
		incomingMD,
		requestProto,
		core.ExpectationUnary,
	)
	if err != nil {
		log.Printf("grpchandler-unary: Error finding matching expectation for %s: %v", fullMethodName, err)
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, "", false)
		return nil, nil, status.Errorf(codes.Internal, "error finding matching expectation: %v", err)
	}

	if expectation == nil {
		log.Printf("grpchandler-unary: No matching expectation found for %s", fullMethodName)
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, "", false)
		return nil, nil, status.Errorf(codes.Unimplemented, "no matching expectation for method %s", fullMethodName)
	}

	if expectation.Type != core.ExpectationUnary { // [cite: 374]
		log.Printf("grpchandler-unary: Matched expectation %s for %s is not of type UNARY (type: %s)", expectation.ID, fullMethodName, expectation.Type)
		RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, expectation.ID, false)
		return nil, nil, status.Errorf(codes.Internal, "matched expectation is not for unary calls")
	}

	currentMatchCount, err := h.storageService.IncrementMatchCount(ctx, expectation.ID)
	if err != nil {
		log.Printf("grpchandler-unary: Failed to increment match count for expectation %s: %v", expectation.ID, err)
	}
	if expectation.ExpectedCallTimes != nil { // [cite: 375]
		if !expectation.ExpectedCallTimes.IsSatisfied(currentMatchCount) { // [cite: 376]
			log.Printf("grpchandler-unary: Expectation %s for %s call count %d exceeds limits", expectation.ID, fullMethodName, currentMatchCount) // [cite: 378]
			RecordFailedCall(ctx, h.storageService, fullMethodName, core.ExpectationUnary, incomingMD, requestProto, expectation.ID, false)        // [cite: 380]
			return nil, nil, status.Errorf(codes.ResourceExhausted, "expectation %s for method %s has been matched its maximum configured times (%d)", expectation.ID, fullMethodName, currentMatchCount-1)
		}
	}

	RecordSuccessfulCall(
		ctx,
		h.storageService,
		fullMethodName,
		core.ExpectationUnary,
		incomingMD,
		requestProto,
		expectation.ID,
	)

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

	log.Printf("grpchandler-unary: Returning success response for %s (expectation %s)", fullMethodName, expectation.ID) // [cite: 381]
	return action.Body, responseHeaders, nil
}
