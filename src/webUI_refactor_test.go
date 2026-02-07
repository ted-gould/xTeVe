package src

import (
	"fmt"
	"testing"
	"time"
)

func TestAddNotificationLimit(t *testing.T) {
	// Initialize System.Notification
	System.Notification = make(map[string]Notification)

	// Add 20 notifications
	for i := 0; i < 20; i++ {
		err := addNotification(Notification{
			Headline: fmt.Sprintf("Headline %d", i),
			Message:  fmt.Sprintf("Message %d", i),
			Type:     "info",
		})
		if err != nil {
			t.Fatalf("Failed to add notification: %v", err)
		}
		// Ensure time advances so keys are unique and ordered
		time.Sleep(2 * time.Millisecond)
	}

	if len(System.Notification) != 10 {
		t.Errorf("Expected 10 notifications, got %d", len(System.Notification))
	}

	// Check if the correct notifications remain (should be 10 to 19)
	for i := 0; i < 10; i++ {
		found := false
		targetHeadline := fmt.Sprintf("Headline %d", i)
		for _, n := range System.Notification {
			if n.Headline == targetHeadline {
				found = true
				break
			}
		}
		if found {
			t.Errorf("Notification '%s' should have been deleted", targetHeadline)
		}
	}

	for i := 10; i < 20; i++ {
		found := false
		targetHeadline := fmt.Sprintf("Headline %d", i)
		for _, n := range System.Notification {
			if n.Headline == targetHeadline {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Notification '%s' should exist", targetHeadline)
		}
	}
}
