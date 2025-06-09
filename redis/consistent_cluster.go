package redis

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"sync"
)

var (
	// ErrNoSlotRanges is returned when no slot ranges are available
	ErrNoSlotRanges = errors.New("redis: no slot ranges available")
)

// slotRange represents a range of slots in the Redis cluster
type slotRange struct {
	startSlot uint16 // Starting slot number (inclusive)
	endSlot   uint16 // Ending slot number (inclusive)
	client    *Client // Client for this slot range
}

// ConsistentClusterClient implements a Redis Cluster client with consistent hashing
// for slot allocation.
type ConsistentClusterClient struct {
	client     *ClusterClient
	slotRanges []slotRange
	mu         sync.RWMutex
}

// NewConsistentClusterClient creates a new ConsistentClusterClient from a ClusterOptions.
func NewConsistentClusterClient(opt *ClusterOptions) (*ConsistentClusterClient, error) {
	client := NewClusterClient(opt)
	cc := &ConsistentClusterClient{
		client: client,
	}

	// Initialize slot ranges
	ctx := context.Background()
	err := cc.ReloadState(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}

	return cc, nil
}

// ReloadState reloads the cluster state and updates slot ranges.
func (cc *ConsistentClusterClient) ReloadState(ctx context.Context) error {
	state, err := cc.client.LoadState(ctx)
	if err != nil {
		return err
	}

	var ranges []slotRange
	for _, slots := range state.slots {
		for i := 0; i < len(slots); i += 2 {
			start := uint16(slots[i])
			end := uint16(slots[i+1])

			client := state.slotMasterNode(int(start)).Client
			ranges = append(ranges, slotRange{
				startSlot: start,
				endSlot:   end,
				client:    client,
			})
		}
	}

	// Sort ranges by start slot
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].startSlot < ranges[j].startSlot
	})

	cc.mu.Lock()
	cc.slotRanges = ranges
	cc.mu.Unlock()

	return nil
}

// Close closes the client, releasing any open resources.
func (cc *ConsistentClusterClient) Close() error {
	return cc.client.Close()
}

// getSlotRange returns the appropriate slot range for a given key using consistent hashing.
func (cc *ConsistentClusterClient) getSlotRange(key string) (slotRange, error) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if len(cc.slotRanges) == 0 {
		return slotRange{}, ErrNoSlotRanges
	}

	// Use fnv hash for consistent hashing
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	hash := h.Sum64()

	// Select slot range based on hash
	index := hash % uint64(len(cc.slotRanges))
	return cc.slotRanges[int(index)], nil
}

// Set sets a key/value pair in the Redis cluster.
func (cc *ConsistentClusterClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *StatusCmd {
	sr, err := cc.getSlotRange(key)
	if err != nil {
		cmd := NewStatusCmd(ctx, "set", key)
		cmd.SetErr(err)
		return cmd
	}
	return sr.client.Set(ctx, key, value, expiration)
}

// Get retrieves a value for a given key from the Redis cluster.
func (cc *ConsistentClusterClient) Get(ctx context.Context, key string) *StringCmd {
	sr, err := cc.getSlotRange(key)
	if err != nil {
		cmd := NewStringCmd(ctx, "get", key)
		cmd.SetErr(err)
		return cmd
	}
	return sr.client.Get(ctx, key)
}

// Del deletes keys from the Redis cluster.
func (cc *ConsistentClusterClient) Del(ctx context.Context, keys ...string) *IntCmd {
	if len(keys) == 0 {
		return NewIntCmd(ctx, "del")
	}

	// For simplicity, use the first key to determine the slot range
	sr, err := cc.getSlotRange(keys[0])
	if err != nil {
		cmd := NewIntCmd(ctx, "del", keys)
		cmd.SetErr(err)
		return cmd
	}
	return sr.client.Del(ctx, keys...)
}

// Ping pings the Redis cluster.
func (cc *ConsistentClusterClient) Ping(ctx context.Context) *StringCmd {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if len(cc.slotRanges) == 0 {
		cmd := NewStringCmd(ctx, "ping")
		cmd.SetErr(ErrNoSlotRanges)
		return cmd
	}

	// Use the first available node
	return cc.slotRanges[0].client.Ping(ctx)
}
