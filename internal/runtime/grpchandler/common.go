package grpchandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/google/uuid" // Added for generating call IDs
	"github.com/rbroggi/grpcmock/internal/runtime/core"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type matcherService interface {
	FindMatchingExpectation(
		ctx context.Context,
		fullMethodName string,
		headers metadata.MD,
		requestBody proto.Message,
		streamType core.ExpectationType,
	) (*core.Expectation, int, error)
}

// store defines the subset of storage.Store methods needed by grpchandler utilities.
// This internal interface can be used if we don't want to import the full storage.Store
// directly into common, or if only a subset of methods are needed by these helpers.
// However, it's often simpler to just use the actual storage.Store interface.
type callRecorderStore interface {
	RecordCall(ctx context.Context, call *core.RecordedCall) error
}

// ConvertProtoToMap converts a proto.Message to map[string]interface{} using protojson.
// This is used to get a comparable representation of the actual request body.
// Also used by the caller (generated server code) if it needs to record the request body.
func ConvertProtoToMap(p proto.Message) (map[string]interface{}, error) {
	if p == nil || reflect.ValueOf(p).IsNil() { //
		return make(map[string]interface{}), nil // Represent nil/empty proto as empty map
	}

	marshaler := protojson.MarshalOptions{ //
		UseProtoNames:   false,
		EmitUnpopulated: true,
	}
	jsonBytes, err := marshaler.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("grpchandler: protojson marshal error: %w", err)
	}

	if string(jsonBytes) == "{}" || string(jsonBytes) == "null" { //
		return make(map[string]interface{}), nil
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &resultMap); err != nil { //
		return nil, fmt.Errorf("grpchandler: json unmarshal to map error: %w (json: %s)", err, string(jsonBytes))
	}
	return resultMap, nil
}

// UnmarshalMapToProto takes a map (typically from core.ResponseAction.Body)
// and unmarshals it into the provided proto.Message instance.
func UnmarshalMapToProto(bodyMap map[string]interface{}, targetProto proto.Message) error {
	if bodyMap == nil {
		if targetProto == nil || reflect.ValueOf(targetProto).IsNil() { //
			return fmt.Errorf("grpchandler: target proto message is nil")
		}
		return protojson.Unmarshal([]byte("{}"), targetProto)
	}

	jsonBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("grpchandler: failed to marshal bodyMap to JSON: %w", err)
	}

	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(jsonBytes, targetProto); err != nil { //
		return fmt.Errorf("grpchandler: failed to unmarshal JSON to proto message %T: %w (json: %s)", targetProto, err, string(jsonBytes))
	}
	return nil
}

// RecordSuccessfulCall creates and stores a record of a successful gRPC call that matched an expectation.
func RecordSuccessfulCall(
	ctx context.Context,
	store callRecorderStore, // Use the defined interface
	fullMethodName string,
	callType core.ExpectationType,
	md metadata.MD,
	reqBodyProto proto.Message,
	expectationID string,
) {
	if store == nil {
		log.Println("grpchandler-record: Warning - storage service is nil, cannot record successful call.")
		return
	}
	reqMap, err := ConvertProtoToMap(reqBodyProto)
	if err != nil {
		log.Printf("grpchandler-record: Failed to convert request proto to map for recording successful call %s: %v", fullMethodName, err)
		// Potentially record with an error marker or skip body
	}
	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID,
		FullMethodName: fullMethodName,
		Type:           callType,
		Headers:        md.Copy(), // Make a copy of metadata
		RequestBody:    reqMap,
		Timestamp:      time.Now().UnixNano(),
		Matched:        true,
	}
	if err := store.RecordCall(ctx, call); err != nil { //
		log.Printf("grpchandler-record: Failed to record successful call for %s: %v", fullMethodName, err)
	} else {
		log.Printf("grpchandler-record: Successfully recorded call for %s, matched expectation %s", fullMethodName, expectationID)
	}
}

// RecordFailedCall creates and stores a record of a gRPC call that failed to match an expectation,
// or matched but failed due to other reasons (e.g., call limits).
func RecordFailedCall(
	ctx context.Context,
	store callRecorderStore, // Use the defined interface
	fullMethodName string,
	callType core.ExpectationType,
	md metadata.MD,
	reqBodyProto proto.Message, // Can be nil if the call itself failed very early
	expectationID string, // ID of the expectation it might have tentatively matched, or empty
	matchedButFailedConstraint bool, // True if it matched an expectation but a constraint (e.g. call times) failed
) {
	if store == nil {
		log.Println("grpchandler-record: Warning - storage service is nil, cannot record failed/unmatched call.")
		return
	}
	var reqMap map[string]interface{}
	var convErr error
	if reqBodyProto != nil {
		reqMap, convErr = ConvertProtoToMap(reqBodyProto)
		if convErr != nil {
			log.Printf("grpchandler-record: Failed to convert request proto to map for recording failed call %s: %v", fullMethodName, convErr)
		}
	}

	call := &core.RecordedCall{
		ID:             uuid.NewString(),
		ExpectationID:  expectationID,
		FullMethodName: fullMethodName,
		Type:           callType,
		Headers:        md.Copy(),
		RequestBody:    reqMap,
		Timestamp:      time.Now().UnixNano(),
		Matched:        matchedButFailedConstraint,
	}
	if err := store.RecordCall(ctx, call); err != nil { //
		log.Printf("grpchandler-record: Failed to record failed/unmatched call for %s: %v", fullMethodName, err)
	} else {
		if expectationID != "" && matchedButFailedConstraint {
			log.Printf("grpchandler-record: Recorded call for %s, matched expectation %s but failed constraint.", fullMethodName, expectationID)
		} else if expectationID != "" { // Matched but something else went wrong if not constraint
			log.Printf("grpchandler-record: Recorded call for %s, associated with expectation %s (match status: %t).", fullMethodName, expectationID, matchedButFailedConstraint)
		} else {
			log.Printf("grpchandler-record: Recorded unmatched call for %s.", fullMethodName)
		}
	}
}
