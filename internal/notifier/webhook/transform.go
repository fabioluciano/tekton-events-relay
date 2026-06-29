package webhook

import (
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
)

// ApplyTransform applies a compiled, trusted administrator-configured gojq query to the input payload.
// Returns the transformed output or an error if the transform fails.
//
// Behavioral Rules:
// - Multiple results: ERROR - jq expressions producing >1 result are rejected
// - Zero results: ERROR - transform must produce exactly 1 result
// - Nil output: ERROR - empty payloads are not allowed
// - Non-JSON-serializable output: ERROR
func ApplyTransform(compiled *gojq.Code, input map[string]any) (any, error) {
	if compiled == nil {
		return input, nil
	}

	// Run the compiled query against the input
	iter := compiled.Run(input)

	// Collect results - we expect exactly 1
	var results []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}

		// Check for errors from the jq execution
		if err, isErr := v.(error); isErr {
			return nil, fmt.Errorf("transform execution error: %w", err)
		}

		results = append(results, v)
	}

	// Validate result count
	if len(results) == 0 {
		return nil, fmt.Errorf("transform produced no results")
	}
	if len(results) > 1 {
		return nil, fmt.Errorf("transform produced %d results, exactly 1 required", len(results))
	}

	output := results[0]

	// Check for nil output
	if output == nil {
		return nil, fmt.Errorf("transform produced nil output; empty payloads are not allowed")
	}

	// Verify JSON serializability
	if _, err := json.Marshal(output); err != nil {
		return nil, fmt.Errorf("transform produced non-JSON-serializable output (type %T): %w", output, err)
	}

	return output, nil
}
