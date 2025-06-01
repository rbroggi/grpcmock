package grpchandler

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

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

// ConvertProtoToMap converts a proto.Message to map[string]interface{} using protojson.
// This is used to get a comparable representation of the actual request body.
// Also used by the caller (generated server code) if it needs to record the request body.
func ConvertProtoToMap(p proto.Message) (map[string]interface{}, error) {
	if p == nil || reflect.ValueOf(p).IsNil() {
		return make(map[string]interface{}), nil // Represent nil/empty proto as empty map
	}

	// Consistent options matching those used in the `matching` service.
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   false,
		EmitUnpopulated: true,
	}
	jsonBytes, err := marshaler.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("grpchandler: protojson marshal error: %w", err)
	}

	if string(jsonBytes) == "{}" || string(jsonBytes) == "null" { // Empty message like ptypes.Empty
		return make(map[string]interface{}), nil
	}

	var resultMap map[string]interface{}
	// Use a json unmarshaler that handles numbers as json.Number to preserve precision if needed,
	// though for map[string]interface{} they usually become float64.
	// Standard json.Unmarshal is typically fine here.
	if err := json.Unmarshal(jsonBytes, &resultMap); err != nil {
		return nil, fmt.Errorf("grpchandler: json unmarshal to map error: %w (json: %s)", err, string(jsonBytes))
	}
	return resultMap, nil
}

// UnmarshalMapToProto takes a map (typically from core.ResponseAction.Body)
// and unmarshals it into the provided proto.Message instance.
func UnmarshalMapToProto(bodyMap map[string]interface{}, targetProto proto.Message) error {
	if bodyMap == nil {
		// If the bodyMap is nil, it implies an empty message.
		// Ensure targetProto is reset or considered empty.
		// Most proto unmarshalers handle nil input as clearing the message.
		// For safety, one might explicitly reset targetProto if possible,
		// but protojson.Unmarshal should handle it.
		// If targetProto is for example *emptypb.Empty, this is fine.
		if targetProto == nil || reflect.ValueOf(targetProto).IsNil() {
			return fmt.Errorf("grpchandler: target proto message is nil")
		}
		// Unmarshal an empty JSON object into it to clear it / set defaults
		return protojson.Unmarshal([]byte("{}"), targetProto)
	}

	jsonBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("grpchandler: failed to marshal bodyMap to JSON: %w", err)
	}

	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true, // Be lenient with extra fields in the map not in proto
	}
	if err := unmarshaler.Unmarshal(jsonBytes, targetProto); err != nil {
		return fmt.Errorf("grpchandler: failed to unmarshal JSON to proto message %T: %w (json: %s)", targetProto, err, string(jsonBytes))
	}
	return nil
}
