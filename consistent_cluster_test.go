package redis

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockRedisServer simulates a Redis cluster node
type mockRedisServer struct {
	listener net.Listener
	slots    map[int]bool
}

func startMockRedisServer(t *testing.T, addr string, slots []int) (*mockRedisServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	slotMap := make(map[int]bool)
	for _, slot := range slots {
		slotMap[slot] = true
	}

	server := &mockRedisServer{
		listener: listener,
		slots:    slotMap,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.handleConnection(conn)
		}
	}()

	return server, nil
}

func (s *mockRedisServer) Close() {
	s.listener.Close()
}

func (s *mockRedisServer) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *mockRedisServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		cmd := string(buf[:n])

		if strings.Contains(cmd, "PING") {
			conn.Write([]byte("+PONG\r\n"))
		} else if strings.Contains(cmd, "CLUSTER SLOTS") {
			// Respond with mock cluster slots information
			// Format: *<number of ranges>
			//         *<range info array size>
			//         :<start slot>
			//         :<end slot>
			//         *<master info array size>
			//         $<ip len>
			//         <ip>
			//         :<port>
			//         ...similar for replicas...

			resp := "*3\r\n"

			// Range 1: 0-5460
			resp += "*3\r\n:0\r\n:5460\r\n*2\r\n$9\r\n127.0.0.1\r\n:" +
				strings.Split(s.listener.Addr().String(), ":")[1] + "\r\n"

			// Range 2: 5461-10922
			resp += "*3\r\n:5461\r\n:10922\r\n*2\r\n$9\r\n127.0.0.1\r\n:" +
				strings.Split(s.listener.Addr().String(), ":")[1] + "\r\n"

			// Range 3: 10923-16383
			resp += "*3\r\n:10923\r\n:16383\r\n*2\r\n$9\r\n127.0.0.1\r\n:" +
				strings.Split(s.listener.Addr().String(), ":")[1] + "\r\n"

			conn.Write([]byte(resp))
		} else if strings.Contains(cmd, "GET") {
			conn.Write([]byte("$5\r\nvalue\r\n"))
		} else if strings.Contains(cmd, "SET") {
			conn.Write([]byte("+OK\r\n"))
		} else if strings.Contains(cmd, "DEL") {
			conn.Write([]byte(":1\r\n"))
		} else {
			// Default OK response
			conn.Write([]byte("+OK\r\n"))
		}
	}
}

func TestConsistentClusterClient(t *testing.T) {
	// Start mock Redis server
	srv, err := startMockRedisServer(t, "127.0.0.1:0", []int{0, 5461, 10923})
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer srv.Close()

	// Create client
	opt := &ClusterOptions{
		Addrs:        []string{srv.Addr().String()},
		MaxRetries:   3,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}

	cc, err := NewConsistentClusterClient(opt)
	if err != nil {
		t.Fatalf("Failed to create ConsistentClusterClient: %v", err)
	}
	defer cc.Close()

	ctx := context.Background()

	// Test basic operations
	t.Run("Set and Get", func(t *testing.T) {
		// Set a key
		err := cc.Set(ctx, "test-key", "test-value", 0).Err()
		if err != nil {
			t.Fatalf("Failed to set key: %v", err)
		}

		// Get the key
		val, err := cc.Get(ctx, "test-key").Result()
		if err != nil {
			t.Fatalf("Failed to get key: %v", err)
		}

		if val != "value" { // Mock server always returns "value"
			t.Errorf("Expected 'value', got '%s'", val)
		}
	})

	t.Run("Del", func(t *testing.T) {
		// Delete a key
		count, err := cc.Del(ctx, "test-key").Result()
		if err != nil {
			t.Fatalf("Failed to delete key: %v", err)
		}

		if count != 1 { // Mock server always returns 1
			t.Errorf("Expected del count 1, got %d", count)
		}
	})

	t.Run("Ping", func(t *testing.T) {
		// Ping the server
		pong, err := cc.Ping(ctx).Result()
		if err != nil {
			t.Fatalf("Failed to ping: %v", err)
		}

		if pong != "PONG" {
			t.Errorf("Expected 'PONG', got '%s'", pong)
		}
	})

	t.Run("Distribution", func(t *testing.T) {
		// Test key distribution
		keyCount := 1000
		slotCounts := make(map[uint16]int)

		// Create many keys and check their distribution
		for i := 0; i < keyCount; i++ {
			key := fmt.Sprintf("key-%d", i)
			sr, err := cc.getSlotRange(key)
			if err != nil {
				t.Fatalf("Failed to get slot range: %v", err)
			}
			slotCounts[sr.startSlot]++
		}

		// Verify distribution is somewhat even (this is approximate)
		t.Logf("Slot distribution for %d keys:", keyCount)
		for slot, count := range slotCounts {
			t.Logf("Slot %d: %d keys (%.2f%%)", slot, count, float64(count)/float64(keyCount)*100)
		}

		// Check that no slot has more than 50% of the keys (very conservative threshold)
		for slot, count := range slotCounts {
			if float64(count)/float64(keyCount) > 0.5 {
				t.Errorf("Slot %d has too many keys: %d (%.2f%%)",
					slot, count, float64(count)/float64(keyCount)*100)
			}
		}
	})

	t.Run("Concurrent operations", func(t *testing.T) {
		var wg sync.WaitGroup
		keyCount := 100
		errCh := make(chan error, keyCount)

		for i := 0; i < keyCount; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := fmt.Sprintf("concurrent-key-%d", i)
				err := cc.Set(ctx, key, fmt.Sprintf("value-%d", i), 0).Err()
				if err != nil {
					errCh <- fmt.Errorf("failed to set %s: %v", key, err)
					return
				}

				_, err = cc.Get(ctx, key).Result()
				if err != nil {
					errCh <- fmt.Errorf("failed to get %s: %v", key, err)
					return
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Error(err)
		}
	})
}

// TestConsistentClusterClient_ReloadState tests that the client can reload state
func TestConsistentClusterClient_ReloadState(t *testing.T) {
	// Start mock Redis server
	srv, err := startMockRedisServer(t, "127.0.0.1:0", []int{0, 5461, 10923})
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	defer srv.Close()

	// Create client
	opt := &ClusterOptions{
		Addrs:        []string{srv.Addr().String()},
		MaxRetries:   3,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}

	cc, err := NewConsistentClusterClient(opt)
	if err != nil {
		t.Fatalf("Failed to create ConsistentClusterClient: %v", err)
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

// Add test for error handling in getSlotRange
func TestConsistentClusterClient_EmptySlotRanges(t *testing.T) {
	cc := &ConsistentClusterClient{
		slotRanges: []slotRange{},
	}

	_, err := cc.getSlotRange("test-key")
	if err == nil {
		t.Error("Expected error for empty slot ranges")
	}
	if err != ErrNoSlotRanges {
		t.Errorf("Expected ErrNoSlotRanges, got: %v", err)
	}
}
