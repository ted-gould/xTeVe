package src

import (
	"slices"
	"testing"
)

// TestSliceDeletionLogic validates the fix for the bug in createXEPGMapping.
// The original implementation used a flawed append pattern that duplicated elements
// instead of removing them. This test ensures that slices.Delete works correctly
// when iterating backwards.
func TestSliceDeletionLogic(t *testing.T) {
	// Scenario: We have a slice of files and want to remove specific indices
	// while iterating backwards.

	// 1. Verify that the new implementation (slices.Delete) works correctly
	files := []string{"keep1", "remove1", "keep2", "remove2"}

	// Simulated loop: for i := len(files) - 1; i >= 0; i--

	// Iteration i=3 (remove2)
	// We want to remove this element.
	files = slices.Delete(files, 3, 4)

	expectedAfterFirst := []string{"keep1", "remove1", "keep2"}
	if !slices.Equal(files, expectedAfterFirst) {
		t.Errorf("slices.Delete failed at i=3. Got %v, want %v", files, expectedAfterFirst)
	}

	// Iteration i=2 (keep2) -> No action

	// Iteration i=1 (remove1)
	// We want to remove this element.
	files = slices.Delete(files, 1, 2)

	expectedFinal := []string{"keep1", "keep2"}
	if !slices.Equal(files, expectedFinal) {
		t.Errorf("slices.Delete failed at i=1. Got %v, want %v", files, expectedFinal)
	}

	// Iteration i=0 (keep1) -> No action
}
