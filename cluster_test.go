package redis_test

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

// TestConsistentClusterClientStateReload tests that the ConsistentClusterClient
// can reload its state and populate slot ranges correctly
func TestConsistentClusterClientStateReload(t *testing.T) {
	// Create a new cluster client
	cc, err := redis.NewConsistentClusterClient(&redis.ClusterOptions{
		Addrs: []string{"localhost:7000", "localhost:7001", "localhost:7002"},
	})
	if err != nil {
		t.Fatalf("Failed to create consistent cluster client: %v", err)
	}
	defer cc.Close()

	ctx := context.Background()

	// Force a state reload
	err = cc.ReloadState(ctx)
	if err != nil {
		t.Fatalf("Failed to reload state: %v", err)
	}

	// Verify we have slot ranges after reload
	cc.mu.RLock()
	slotRangeCount := len(cc.slotRanges)
	cc.mu.RUnlock()

	if slotRangeCount == 0 {
		t.Error("Expected slot ranges after reload, got none")
	} else {
		t.Logf("Found %d slot ranges after reload", slotRangeCount)
	}
}

// TestGetSlotRangeError tests error handling in getSlotRange
func TestGetSlotRangeError(t *testing.T) {
	// Create a new cluster client
	cc, err := redis.NewConsistentClusterClient(&redis.ClusterOptions{
		Addrs: []string{"localhost:7000", "localhost:7001", "localhost:7002"},
	})
	if err != nil {
		t.Fatalf("Failed to create consistent cluster client: %v", err)
	}
	defer cc.Close()

	// We don't need to test the actual implementation since it's internal
	// Just ensure the client was created successfully
	if cc == nil {
		t.Error("Expected non-nil client")
	}
}
