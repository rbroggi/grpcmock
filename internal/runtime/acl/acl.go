package acl

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rbroggi/grpcmock/internal/runtime/api"
	"github.com/rbroggi/grpcmock/internal/runtime/core"
)

// ToInternalExpectation translates an API GRPCCallExpectation DTO to an internal core.Expectation.
func ToInternalExpectation(apiExp *api.GRPCCallExpectation) (*core.Expectation, error) {
	if apiExp == nil {
		return nil, errors.New("API expectation is nil")
	}
	if apiExp.FullMethodName == "" {
		return nil, errors.New("fullMethodName is required in expectation")
	}

	coreExp := &core.Expectation{
		ID:             apiExp.ID, // User might suggest an ID, or we generate one
		FullMethodName: apiExp.FullMethodName,
	}

	if coreExp.ID == "" {
		coreExp.ID = uuid.NewString()
	}

	// Translate RequestMatcher
	if apiExp.RequestMatcher != nil {
		coreReqCondition, err := translateRequestMatcher(apiExp.RequestMatcher)
		if err != nil {
			return nil, fmt.Errorf("failed to translate requestMatcher: %w", err)
		}
		coreExp.RequestCondition = coreReqCondition
	}

	// Translate ExpectedCallTimes
	if apiExp.ExpectedCallTimes != nil {
		coreExp.ExpectedCallTimes = &core.ExpectedCallTimes{
			Min: apiExp.ExpectedCallTimes.Min,
			Max: apiExp.ExpectedCallTimes.Max,
		}
	}

	// Determine type and translate response/stream actions
	if apiExp.StreamMock != nil && (len(apiExp.StreamMock.Responses) > 0 || len(apiExp.StreamMock.ExpectedRequests) > 0) {
		// This looks like a streaming expectation
		if len(apiExp.StreamMock.ExpectedRequests) > 0 && len(apiExp.StreamMock.Responses) == 0 {
			coreExp.Type = core.ExpectationClientStream
			// For client streaming, the main 'apiExp.Response' is the single response after client finishes.
			if apiExp.Response != nil {
				respAction, err := translateResponseAction(apiExp.Response)
				if err != nil {
					return nil, fmt.Errorf("failed to translate final response for client stream: %w", err)
				}
				coreExp.ResponseAction = respAction
			} else {
				// It's valid for a client stream to have no server response (e.g. just an ACK or error)
				// but our model requires a ResponseAction. We can create an empty one or error.
				// For now, let's assume an error is preferred if no explicit final response.
				return nil, errors.New("client stream expectation defined via StreamMock.ExpectedRequests must also have a final 'response' field in the top-level expectation for the server's response")
			}
			// Translate sequence of expected client messages
			for _, apiReqMatcher := range apiExp.StreamMock.ExpectedRequests {
				cReqCond, err := translateRequestMatcher(&apiReqMatcher) // Pass as pointer
				if err != nil {
					return nil, fmt.Errorf("failed to translate client stream expected request: %w", err)
				}
				coreExp.StreamActions = append(coreExp.StreamActions, core.StreamAction{Receive: &cReqCond})
			}

		} else if len(apiExp.StreamMock.Responses) > 0 && len(apiExp.StreamMock.ExpectedRequests) == 0 {
			coreExp.Type = core.ExpectationServerStream
			for _, apiResp := range apiExp.StreamMock.Responses {
				respAction, err := translateResponseAction(&apiResp) // Pass as pointer
				if err != nil {
					return nil, fmt.Errorf("failed to translate server stream response: %w", err)
				}
				coreExp.StreamActions = append(coreExp.StreamActions, core.StreamAction{Send: &respAction})
			}
		} else if len(apiExp.StreamMock.Responses) > 0 && len(apiExp.StreamMock.ExpectedRequests) > 0 {
			coreExp.Type = core.ExpectationBidiStream
			// For Bidi, the order in StreamMock.Responses and StreamMock.ExpectedRequests
			// doesn't inherently define the sequence. The current api.StreamMock is not ideal for bidi.
			// A better api.StreamMock for bidi would be a list of actions, each specifying send or expect.
			// For now, this translation will be limited or may need a convention.
			// Let's assume for now, it's not a strict sequence defined by these two separate arrays for Bidi.
			// This part needs a clearer API design for Bidi sequences or a very specific interpretation.
			// Simplistic: treat all ExpectedRequests as things client might send, and Responses as things server might send.
			// This is NOT sequence-aware for bidi.
			// A true bidi sequence would require a redesign of api.StreamMock or core.StreamAction population.
			// TEMPORARY: We will assume a simple interpretation or error for now.
			return nil, errors.New("bidirectional stream expectations require a more specific sequence definition in StreamMock (e.g., a single list of send/receive actions); current structure is ambiguous for bidi")
		} else {
			// StreamMock is present but empty, this might be an error or imply default streaming behavior.
			return nil, errors.New("StreamMock is present but both Responses and ExpectedRequests are empty")
		}
	} else if apiExp.Response != nil {
		coreExp.Type = core.ExpectationUnary
		respAction, err := translateResponseAction(apiExp.Response)
		if err != nil {
			return nil, fmt.Errorf("failed to translate unary response: %w", err)
		}
		coreExp.ResponseAction = respAction
	} else {
		return nil, errors.New("expectation must have either a 'response' (for unary) or a 'streamMock' (for streaming)")
	}

	return coreExp, nil
}

func translateRequestMatcher(apiRM *api.RequestMatcher) (core.RequestCondition, error) {
	var coreRC core.RequestCondition
	if apiRM == nil {
		return coreRC, nil // Empty condition means match anything
	}

	// Translate BodyMatcher
	if apiRM.BodyMatcher != nil {
		coreBM, err := translateBodyMatcher(apiRM.BodyMatcher)
		if err != nil {
			return coreRC, fmt.Errorf("failed to translate bodyMatcher: %w", err)
		}
		coreRC.BodyMatcher = coreBM
	}

	// Translate HeadersMatcher (simple map first, then advanced if present)
	coreRC.HeadersMatcher.Fields = make(map[string]core.FieldMatcher)
	if len(apiRM.Headers) > 0 && apiRM.AdvancedHeadersMatcher != nil && len(apiRM.AdvancedHeadersMatcher.Fields) > 0 {
		return coreRC, errors.New("cannot specify both 'headers' (simple map) and 'advancedHeadersMatcher' in a RequestMatcher; use one")
	}

	if len(apiRM.Headers) > 0 {
		for k, v := range apiRM.Headers {
			coreRC.HeadersMatcher.Fields[k] = core.FieldMatcher{Equals: v} // Simple map implies exact match
		}
	} else if apiRM.AdvancedHeadersMatcher != nil {
		for k, apiFM := range apiRM.AdvancedHeadersMatcher.Fields {
			coreRC.HeadersMatcher.Fields[k] = core.FieldMatcher{
				Equals:   apiFM.Equals,
				Regex:    apiFM.Regex,
				Contains: apiFM.Contains,
			}
		}
	}
	return coreRC, nil
}

func translateBodyMatcher(apiBM *api.BodyMatcher) (core.BodyMatcher, error) {
	var coreBM core.BodyMatcher

	if apiBM == nil {
		coreBM.Strategy = core.BodyMatchUndefined // Or treat as match-any
		return coreBM, nil
	}

	hasEquals := apiBM.Equals != nil
	hasContains := apiBM.Contains != nil

	if hasEquals && hasContains {
		return coreBM, errors.New("bodyMatcher cannot have both 'equals' and 'contains' fields set; use one")
	}

	if hasEquals {
		expectedMap, ok := apiBM.Equals.(map[string]interface{})
		if !ok {
			// It might be that encoding/json unmarshalled it into json.RawMessage if it was a complex object
			// or if user sent a string for equals, which is not what we want for structured equals.
			// For structured equals, we expect a map.
			return coreBM, errors.New("bodyMatcher.equals must be a JSON object for structured matching")
		}
		coreBM.Strategy = core.BodyMatchFull
		coreBM.ExpectedBody = expectedMap
	} else if hasContains {
		expectedMap, ok := apiBM.Contains.(map[string]interface{})
		if !ok {
			return coreBM, errors.New("bodyMatcher.contains must be a JSON object for subset matching")
		}
		coreBM.Strategy = core.BodyMatchSubset
		coreBM.ExpectedBody = expectedMap
	} else {
		coreBM.Strategy = core.BodyMatchUndefined // No specific body matching
	}

	return coreBM, nil
}

func translateResponseAction(apiResp *api.MockResponse) (core.ResponseAction, error) {
	var coreRA core.ResponseAction
	if apiResp == nil {
		return coreRA, errors.New("API MockResponse is nil")
	}

	coreRA.Headers = apiResp.Headers // Direct copy
	coreRA.Delay = apiResp.Delay

	if apiResp.Error != nil {
		coreRA.Error = &core.RPCError{
			Code:    apiResp.Error.Code,
			Message: apiResp.Error.Message,
		}
	}

	// For Body, apiResp.Body is json.RawMessage. We need to unmarshal it to map[string]interface{}
	// if it's not empty, to allow internal handlers to potentially work with it as a map,
	// before it's re-marshalled into a specific proto type by the generated server code.
	if len(apiResp.Body) > 0 && string(apiResp.Body) != "null" {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(apiResp.Body, &bodyMap); err != nil {
			// It could be a non-object JSON like a string, number, or array.
			// For now, our core.ResponseAction.Body expects map[string]interface{} for proto messages.
			// If the intent is to return a raw JSON string or other primitive, the model might need adjustment.
			// Or, we treat it as an opaque RawMessage if it's not a map.
			// For simplicity, let's assume response bodies are typically objects.
			return coreRA, fmt.Errorf("failed to unmarshal response body from json.RawMessage to map[string]interface{}: %w. Body must be a JSON object", err)
		}
		coreRA.Body = bodyMap
	}
	if coreRA.Error == nil && coreRA.Body == nil && len(apiResp.Body) > 0 && string(apiResp.Body) != "null" {
		// This case means Unmarshal failed, but no error was set if the body was not a map.
		// This indicates an issue with the assumption above or the input.
	}

	return coreRA, nil
}

// ToAPIExpectation translates an internal core.Expectation to an API GRPCCallExpectation DTO.
// This is a simplified reverse translation, focusing on key fields.
func ToAPIExpectation(coreExp *core.Expectation) (*api.GRPCCallExpectation, error) {
	if coreExp == nil {
		return nil, errors.New("core expectation is nil")
	}

	apiExp := &api.GRPCCallExpectation{
		ID:             coreExp.ID,
		FullMethodName: coreExp.FullMethodName,
	}

	if coreExp.ExpectedCallTimes != nil {
		apiExp.ExpectedCallTimes = &api.ExpectedCallTimes{
			Min: coreExp.ExpectedCallTimes.Min,
			Max: coreExp.ExpectedCallTimes.Max,
		}
	}

	apiReqMatcher, err := toAPIRequestMatcher(&coreExp.RequestCondition)
	if err != nil {
		return nil, fmt.Errorf("failed to translate core.RequestCondition to api.RequestMatcher: %w", err)
	}
	apiExp.RequestMatcher = apiReqMatcher

	// Translate based on type
	switch coreExp.Type {
	case core.ExpectationUnary:
		apiResp, err := toAPIMockResponse(&coreExp.ResponseAction)
		if err != nil {
			return nil, fmt.Errorf("failed to translate core.ResponseAction to api.MockResponse for unary: %w", err)
		}
		apiExp.Response = apiResp
	case core.ExpectationClientStream:
		// Final response
		apiFinalResp, err := toAPIMockResponse(&coreExp.ResponseAction)
		if err != nil {
			return nil, fmt.Errorf("failed to translate core.ResponseAction to api.MockResponse for client stream final response: %w", err)
		}
		apiExp.Response = apiFinalResp

		// Expected client messages
		apiExp.StreamMock = &api.StreamMock{}
		for _, action := range coreExp.StreamActions {
			if action.Receive != nil {
				apiReqMatcher, err := toAPIRequestMatcher(action.Receive)
				if err != nil {
					return nil, fmt.Errorf("failed to translate core.RequestCondition for client stream message: %w", err)
				}
				apiExp.StreamMock.ExpectedRequests = append(apiExp.StreamMock.ExpectedRequests, *apiReqMatcher)
			}
		}
	case core.ExpectationServerStream:
		apiExp.StreamMock = &api.StreamMock{}
		for _, action := range coreExp.StreamActions {
			if action.Send != nil {
				apiResp, err := toAPIMockResponse(action.Send)
				if err != nil {
					return nil, fmt.Errorf("failed to translate core.ResponseAction for server stream message: %w", err)
				}
				apiExp.StreamMock.Responses = append(apiExp.StreamMock.Responses, *apiResp)
			}
		}
	case core.ExpectationBidiStream:
		// This reverse translation is complex due to the bidi ambiguity mentioned earlier.
		// For now, it will be left simplified or omitted.
		apiExp.StreamMock = &api.StreamMock{} // Placeholder
		// Populate ExpectedRequests and Responses based on coreExp.StreamActions
		// This still doesn't capture true bidi sequence well in the current api.StreamMock
		for _, action := range coreExp.StreamActions {
			if action.Receive != nil {
				apiRecReq, err := toAPIRequestMatcher(action.Receive)
				if err == nil {
					apiExp.StreamMock.ExpectedRequests = append(apiExp.StreamMock.ExpectedRequests, *apiRecReq)
				}
			}
			if action.Send != nil {
				apiSendResp, err := toAPIMockResponse(action.Send)
				if err == nil {
					apiExp.StreamMock.Responses = append(apiExp.StreamMock.Responses, *apiSendResp)
				}
			}
		}
	default:
		return nil, fmt.Errorf("unknown core expectation type: %s", coreExp.Type)
	}

	return apiExp, nil
}

func toAPIRequestMatcher(coreRC *core.RequestCondition) (*api.RequestMatcher, error) {
	if coreRC == nil {
		return nil, nil
	}
	apiRM := &api.RequestMatcher{}

	// Body Matcher
	if coreRC.BodyMatcher.Strategy != core.BodyMatchUndefined {
		apiBM := api.BodyMatcher{}
		bodyJSON, err := json.Marshal(coreRC.BodyMatcher.ExpectedBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal core.ExpectedBody to json.RawMessage: %w", err)
		}

		if coreRC.BodyMatcher.Strategy == core.BodyMatchFull {
			// api.BodyMatcher.Equals expects interface{}, but it will be set with json.RawMessage
			// which is fine as it will be marshalled back to JSON correctly.
			// However, to be consistent with API input, we should unmarshal it back to map.
			var bodyMap map[string]interface{}
			if err := json.Unmarshal(bodyJSON, &bodyMap); err == nil {
				apiBM.Equals = bodyMap
			} else {
				// fallback or error
				apiBM.Equals = json.RawMessage(bodyJSON)
			}

		} else if coreRC.BodyMatcher.Strategy == core.BodyMatchSubset {
			var bodyMap map[string]interface{}
			if err := json.Unmarshal(bodyJSON, &bodyMap); err == nil {
				apiBM.Contains = bodyMap
			} else {
				apiBM.Contains = json.RawMessage(bodyJSON)
			}
		}
		apiRM.BodyMatcher = &apiBM
	}

	// Headers Matcher
	if len(coreRC.HeadersMatcher.Fields) > 0 {
		apiHM := api.HeadersMatcher{Fields: make(map[string]api.FieldMatcher)}
		isSimpleHeaders := true
		simpleHeaders := make(map[string]string)

		for k, coreFM := range coreRC.HeadersMatcher.Fields {
			apiHM.Fields[k] = api.FieldMatcher{
				Equals:   coreFM.Equals,
				Regex:    coreFM.Regex,
				Contains: coreFM.Contains,
			}
			// Check if it could have been represented by simple map
			if coreFM.Regex != "" || coreFM.Contains != "" || (coreFM.Equals == "" && (coreFM.Regex != "" || coreFM.Contains != "")) {
				isSimpleHeaders = false
			}
			if isSimpleHeaders && coreFM.Equals != "" {
				simpleHeaders[k] = coreFM.Equals
			} else {
				isSimpleHeaders = false // if a field is not just a simple equals
			}
		}

		if isSimpleHeaders && len(simpleHeaders) == len(coreRC.HeadersMatcher.Fields) {
			apiRM.Headers = simpleHeaders
		} else {
			apiRM.AdvancedHeadersMatcher = &apiHM
		}
	}
	return apiRM, nil
}

func toAPIMockResponse(coreRA *core.ResponseAction) (*api.MockResponse, error) {
	if coreRA == nil {
		return nil, errors.New("core ResponseAction is nil")
	}
	apiResp := &api.MockResponse{
		Headers: coreRA.Headers,
		Delay:   coreRA.Delay,
	}

	if coreRA.Error != nil {
		apiResp.Error = &api.RPCError{
			Code:    coreRA.Error.Code,
			Message: coreRA.Error.Message,
		}
	}

	if coreRA.Body != nil {
		bodyBytes, err := json.Marshal(coreRA.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal core.ResponseAction.Body to json.RawMessage: %w", err)
		}
		apiResp.Body = json.RawMessage(bodyBytes)
	}
	return apiResp, nil
}

// ToAPIRecordedCall translates an internal core.RecordedCall to an API RecordedGRPCCall DTO.
func ToAPIRecordedCall(coreCall *core.RecordedCall) (*api.RecordedGRPCCall, error) {
	if coreCall == nil {
		return nil, errors.New("core recorded call is nil")
	}

	apiCall := &api.RecordedGRPCCall{
		ID:             coreCall.ID,
		ExpectationID:  coreCall.ExpectationID,
		FullMethodName: coreCall.FullMethodName,
		Type:           string(coreCall.Type), // Convert core.ExpectationType to string
		Headers:        coreCall.Headers,
		RequestBody:    coreCall.RequestBody, // Assumes RequestBody is already map[string]interface{} or suitable for JSON
		Timestamp:      coreCall.Timestamp,
		Matched:        coreCall.Matched,
	}

	// TODO: Translate StreamMessages if that feature is fully implemented
	// if len(coreCall.StreamMessages) > 0 {
	//  for _, sm := range coreCall.StreamMessages {
	//      apiCall.StreamMessages = append(apiCall.StreamMessages, ...)
	//  }
	// }

	return apiCall, nil
}

// ToCoreRecordedCall translates an API RecordedGRPCCall DTO to an internal core.RecordedCall.
// This is less common as calls are typically recorded internally, not created from API.
// Included for completeness or future use cases.
func ToCoreRecordedCall(apiCall *api.RecordedGRPCCall) (*core.RecordedCall, error) {
	if apiCall == nil {
		return nil, errors.New("API recorded call is nil")
	}

	coreCall := &core.RecordedCall{
		ID:             apiCall.ID,
		ExpectationID:  apiCall.ExpectationID,
		FullMethodName: apiCall.FullMethodName,
		Type:           core.ExpectationType(apiCall.Type), // Convert string to core.ExpectationType
		Headers:        apiCall.Headers,
		RequestBody:    apiCall.RequestBody,
		Timestamp:      apiCall.Timestamp,
		Matched:        apiCall.Matched,
	}
	if coreCall.Type == core.ExpectationTypeUndefined && apiCall.Type != "" {
		return nil, fmt.Errorf("invalid expectation type string: %s", apiCall.Type)
	}

	return coreCall, nil
}
