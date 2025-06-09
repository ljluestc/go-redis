package redis

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9/internal/hashtag"
)

// ErrNoSlotRanges is returned when there are no slot ranges available
var ErrNoSlotRanges = errors.New("no slot ranges available")

// slotRange represents a range of slots in a Redis cluster
type slotRange struct {
	startSlot uint16
	endSlot   uint16
	client    *Client
	hash      string // SHA1 of startSlot:endSlot
}

// ConsistentClusterClient provides a client with consistent hashing for routing
// Redis commands to the appropriate node in a Redis cluster.
type ConsistentClusterClient struct {
	client     *ClusterClient
	slotRanges []slotRange
	mu         sync.RWMutex
}

// NewConsistentClusterClient creates a client with consistent hashing.
// This client ensures keys are consistently routed to the same Redis node.
func NewConsistentClusterClient(opt *ClusterOptions) (*ConsistentClusterClient, error) {
	client := NewClusterClient(opt)
	ctx := context.Background()

	// Test the connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping cluster: %w", err)
	}

	cc := &ConsistentClusterClient{
		client: client,
	}

	if err := cc.updateSlotRanges(ctx); err != nil {
		return nil, err
	}

	// Periodically refresh topology
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-time.After(30 * time.Second):
				if err := cc.updateSlotRanges(ctx); err != nil {
					continue
				}
			case <-ticker.C:
				// Refresh cluster state without handling error
				client.ReloadState(ctx)
				if err := client.ForEachShard(ctx, func(ctx context.Context, shard *Client) error {
					return shard.Ping(ctx).Err()
				}); err != nil {
					continue
				}
			}
		}
	}()

	return cc, nil
}

// updateSlotRanges fetches cluster slots and builds hashable ranges.
func (cc *ConsistentClusterClient) updateSlotRanges(ctx context.Context) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	slots, err := cc.client.ClusterSlots(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to get cluster slots: %w", err)
	}

	var ranges []slotRange
	for _, slotInfo := range slots {
		if len(slotInfo.Nodes) == 0 {
			continue
		}

		// Get the client for the master node
		masterAddr := slotInfo.Nodes[0].Addr
		client, err := cc.client.MasterForKey(ctx, masterAddr)
		if err != nil {
			return err
		}

		hash := fmt.Sprintf("%d:%d", slotInfo.Start, slotInfo.End)
		ranges = append(ranges, slotRange{
			startSlot: uint16(slotInfo.Start),
			endSlot:   uint16(slotInfo.End),
			client:    client,
			hash:      fmt.Sprintf("%x", sha1.Sum([]byte(hash))),
		})
	}

	// Sort by hash for consistent key distribution
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].hash < ranges[j].hash
	})

	cc.slotRanges = ranges
	return nil
}

// getSlotRange returns the slot range for a given key
func (cc *ConsistentClusterClient) getSlotRange(key string) (slotRange, error) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if len(cc.slotRanges) == 0 {
		return slotRange{}, ErrNoSlotRanges
	}

	// Get the slot for the key
	slot := uint16(hashtag.Slot(key))

	// Find the range that contains the slot
	for _, r := range cc.slotRanges {
		if slot >= r.startSlot && slot <= r.endSlot {
			return r, nil
		}
	}

	// If we can't find a range, use consistent hashing
	keyHash := fmt.Sprintf("%x", sha1.Sum([]byte(key)))
	for _, sr := range cc.slotRanges {
		if keyHash <= sr.hash {
			return sr, nil
		}
	}

	return cc.slotRanges[0], nil
}

// mapKeyToSlot uses consistent hashing to map a key to a slot.
// This overrides the default CRC16 based mapping.
func (cc *ConsistentClusterClient) mapKeyToSlot(key string) (int, error) {
	sr, err := cc.getSlotRange(key)
	if err != nil {
		return 0, err
	}

	// Calculate a slot within the range using the key hash
	keyBytes := []byte(key)
	hash := int(keyBytes[0]) // Simple hash for demonstration
	slotRange := int(sr.endSlot) - int(sr.startSlot) + 1
	slot := int(sr.startSlot) + (hash % slotRange)

	return slot, nil
}

// Close closes the client, releasing any open resources
func (cc *ConsistentClusterClient) Close() error {
	return cc.client.Close()
}

// Client returns the underlying ClusterClient
func (cc *ConsistentClusterClient) Client() *ClusterClient {
	return cc.client
}

// Do executes a command using consistent hashing.
func (cc *ConsistentClusterClient) Do(ctx context.Context, args ...interface{}) *Cmd {
	cmd := NewCmd(ctx, args...)
	_ = cc.Process(ctx, cmd)
	return cmd
}

// Process routes a command to the appropriate node based on the key
func (cc *ConsistentClusterClient) Process(ctx context.Context, cmd Cmder) error {
	// For multi-key commands, use the default ClusterClient implementation
	if cc.isMultiKeyCommand(cmd) {
		return cc.client.Process(ctx, cmd)
	}

	// Extract the key from the command
	key := cc.getKey(cmd)
	if key != "" {
		// Map key to slot using consistent hashing
		_, err := cc.mapKeyToSlot(key)
		if err != nil {
			return err
		}
	}

	// Use the first key for routing
	args := cmd.Args()
	if len(args) >= 2 {
		if key, ok := args[1].(string); ok {
			// Get the slot range for the key
			sr, err := cc.getSlotRange(key)
			if err != nil {
				return err
			}

			// Execute the command on the specific client
			return sr.client.Process(ctx, cmd)
		}
	}

	// Default fallback to standard processing
	return cc.client.Process(ctx, cmd)
}

// getKey extracts the key from a command.
func (cc *ConsistentClusterClient) getKey(cmd Cmder) string {
	if len(cmd.Args()) > 1 {
		// Most Redis commands have the key as the first argument
		if key, ok := cmd.Args()[1].(string); ok {
			return key
		}
	}
	return ""
}

// isMultiKeyCommand checks if a command operates on multiple keys.
func (cc *ConsistentClusterClient) isMultiKeyCommand(cmd Cmder) bool {
	// Implement logic to identify multi-key commands
	name := cmd.Name()
	multiKeyCommands := map[string]bool{
		"MGET":     true,
		"MSET":     true,
		"DEL":      true,
		"UNLINK":   true,
		"TOUCH":    true,
		"RENAME":   true,
		"RENAMENX": true,
		"BLPOP":    true,
		"BRPOP":    true,
		"SDIFF":    true,
		"SINTER":   true,
		"SUNION":   true,
		"ZINTER":   true,
		"ZUNION":   true,
	}
	return multiKeyCommands[name]
}

// Pipelined executes multiple commands in a single batch using a pipeline.
func (cc *ConsistentClusterClient) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return cc.client.Pipelined(ctx, fn)
}

// Pipeline creates a new pipeline which is able to execute multiple commands in a single batch.
func (cc *ConsistentClusterClient) Pipeline() Pipeliner {
	return cc.client.Pipeline()
}

// Ping pings all nodes in the cluster.
func (cc *ConsistentClusterClient) Ping(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "ping")
	_ = cc.Process(ctx, cmd)
	return cmd
}

// Get gets the value for a key using consistent hashing.
func (cc *ConsistentClusterClient) Get(ctx context.Context, key string) *StringCmd {
	cmd := NewStringCmd(ctx, "GET", key)
	_ = cc.Process(ctx, cmd)
	return cmd
}

// Set sets a key-value pair using consistent hashing.
func (cc *ConsistentClusterClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *StatusCmd {
	args := make([]interface{}, 0, 4)
	args = append(args, "SET", key, value)
	if expiration > 0 {
		if usePrecise(expiration) {
			args = append(args, "PX", formatMs(ctx, expiration))
		} else {
			args = append(args, "EX", formatSec(ctx, expiration))
		}
	}
	cmd := NewStatusCmd(ctx, args...)
	_ = cc.Process(ctx, cmd)
	return cmd
}

// Del deletes keys.
func (cc *ConsistentClusterClient) Del(ctx context.Context, keys ...string) *IntCmd {
	args := make([]interface{}, 1+len(keys))
	args[0] = "DEL"
	for i, key := range keys {
		args[1+i] = key
	}
	cmd := NewIntCmd(ctx, args...)
	_ = cc.Process(ctx, cmd)
	return cmd
}

// ForEachShard concurrently calls the fn on each live shard in the cluster.
// It returns the first error if any.
func (cc *ConsistentClusterClient) ForEachShard(ctx context.Context, fn func(ctx context.Context, client *Client) error) error {
	return cc.client.ForEachShard(ctx, fn)
}

// ForEachMaster concurrently calls the fn on each master node in the cluster.
// It returns the first error if any.
func (cc *ConsistentClusterClient) ForEachMaster(ctx context.Context, fn func(ctx context.Context, client *Client) error) error {
	return cc.client.ForEachMaster(ctx, fn)
}

// Watch creates a transaction with optimistic locking.
func (cc *ConsistentClusterClient) Watch(ctx context.Context, fn func(*Tx) error, keys ...string) error {
	return cc.client.Watch(ctx, fn, keys...)
}

// ReloadState reloads cluster state.
func (cc *ConsistentClusterClient) ReloadState(ctx context.Context) error {
	// Reload client state and update slot ranges
	cc.client.ReloadState(ctx)
	return cc.updateSlotRanges(ctx)
}
