package src

import (
	"encoding/json"
	"testing"
)

type TestStruct struct {
	Name    string `json:"name"`
	Value   int    `json:"value"`
	Active  bool   `json:"active"`
	Details struct {
		Description string `json:"description"`
	} `json:"details"`
}

func BenchmarkOldMethod(b *testing.B) {
	inputMap := map[string]any{
		"name":   "Benchmark",
		"value":  123,
		"active": true,
		"details": map[string]any{
			"description": "This is a test",
		},
	}
	var output TestStruct

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Old method: map -> indented JSON string -> []byte -> struct
		jsonString := mapToJSON(inputMap)
		_ = json.Unmarshal([]byte(jsonString), &output)
	}
}

func BenchmarkNewMethod(b *testing.B) {
	inputMap := map[string]any{
		"name":   "Benchmark",
		"value":  123,
		"active": true,
		"details": map[string]any{
			"description": "This is a test",
		},
	}
	var output TestStruct

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// New method: bindToStruct (map -> []byte -> struct)
		_ = bindToStruct(inputMap, &output)
	}
}
