package pool_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	. "github.com/bsm/ginkgo/v2"
	. "github.com/bsm/gomega"

	"github.com/redis/go-redis/v9/internal/pool"
)

var _ = Describe("ConnPool", func() {
	ctx := context.Background()
	var connPool *pool.ConnPool

	BeforeEach(func() {
		connPool = pool.NewConnPool(&pool.Options{
			Dialer:          dummyDialer,
			PoolSize:        10,
			PoolTimeout:     time.Hour,
			DialTimeout:     1 * time.Second,
			ConnMaxIdleTime: time.Millisecond,
		})
	})

	AfterEach(func() {
		connPool.Close()
	})

	It("should safe close", func() {
		const minIdleConns = 10

		var (
			wg         sync.WaitGroup
			closedChan = make(chan struct{})
		)
		wg.Add(minIdleConns)
		connPool = pool.NewConnPool(&pool.Options{
			Dialer: func(ctx context.Context) (net.Conn, error) {
				wg.Done()
				<-closedChan
				return &net.TCPConn{}, nil
			},
			PoolSize:        10,
			PoolTimeout:     time.Hour,
			DialTimeout:     1 * time.Second,
			ConnMaxIdleTime: time.Millisecond,
			MinIdleConns:    minIdleConns,
		})
		wg.Wait()
		Expect(connPool.Close()).NotTo(HaveOccurred())
		close(closedChan)

		// We wait for 1 second and believe that checkMinIdleConns has been executed.
		time.Sleep(time.Second)

		Expect(connPool.Stats()).To(Equal(&pool.Stats{
			Hits:           0,
			Misses:         0,
			Timeouts:       0,
			WaitCount:      0,
			WaitDurationNs: 0,
			TotalConns:     0,
			IdleConns:      0,
			StaleConns:     0,
		}))
	})

	It("should unblock client when conn is removed", func() {
		// Reserve one connection.
		cn, err := connPool.Get(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Reserve all other connections.
		var cns []*pool.Conn
		for i := 0; i < 9; i++ {
			cn, err := connPool.Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			cns = append(cns, cn)
		}

		started := make(chan bool, 1)
		done := make(chan bool, 1)
		go func() {
			defer GinkgoRecover()

			started <- true
			_, err := connPool.Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			done <- true

			connPool.Put(ctx, cn)
		}()
		<-started

		// Check that Get is blocked.
		select {
		case <-done:
			Fail("Get is not blocked")
		case <-time.After(time.Millisecond):
			// ok
		}

		connPool.Remove(ctx, cn, nil)

		// Check that Get is unblocked.
		select {
		case <-done:
			// ok
		case <-time.After(time.Second):
			Fail("Get is not unblocked")
		}

		for _, cn := range cns {
			connPool.Put(ctx, cn)
		}
	})
})

var _ = Describe("MinIdleConns", func() {
	const poolSize = 100
	ctx := context.Background()
	var minIdleConns int
	var connPool *pool.ConnPool
package pool_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/internal/pool"
)

func TestCustomHealthCheck(t *testing.T) {
	// Start a mock Redis server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept connections in a separate goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Handle the connection in a separate goroutine
			go handleConnection(conn)
		}
	}()

	// Create a map to track which addresses we consider "draining"
	drainingNodes := &sync.Map{}
	drainingNodes.Store(listener.Addr().String(), false)

	// Create Redis client with custom health check
	opt := &redis.Options{
		Addr: listener.Addr().String(),
		HealthCheckHook: func(ctx context.Context, cn *redis.Conn) bool {
			// Check if this node is marked as draining
			draining, _ := drainingNodes.Load(cn.NetConn().RemoteAddr().String())
			return !draining.(bool)
		},
		DialTimeout: time.Second,
		PoolSize:    10,
	}

	client := redis.NewClient(opt)
	defer client.Close()

	// Test 1: Connection should be healthy (not draining)
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Ping failed when node is healthy: %v", err)
	}

	// Test 2: Mark node as draining, connections should be considered unhealthy
	drainingNodes.Store(listener.Addr().String(), true)

	// Give some time for the change to propagate
	time.Sleep(2 * time.Second)

	// Try multiple times as connection pool might have existing connections
	for i := 0; i < 5; i++ {
		_, err = client.Ping(ctx).Result()
		if err != nil {
			// Expected to fail since node is draining
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err == nil {
		t.Fatalf("Expected Ping to fail when node is draining")
	}

	// Test 3: Mark node as not draining again, connections should recover
	drainingNodes.Store(listener.Addr().String(), false)

	// Give some time for the pool to recover
	time.Sleep(2 * time.Second)

	// Try multiple times as it might take a moment to establish new connections
	var pingSuccess bool
	for i := 0; i < 5; i++ {
		_, err = client.Ping(ctx).Result()
		if err == nil {
			pingSuccess = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !pingSuccess {
		t.Fatalf("Ping failed after node stopped draining: %v", err)
	}
}

// Simple mock Redis server that responds to PING
func handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		// Set a read deadline to prevent hanging
		conn.SetReadDeadline(time.Now().Add(time.Second))
		
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		
		// Check if the command is PING
		if string(buf[:n]) == "*1\r\n$4\r\nPING\r\n" {
			conn.Write([]byte("+PONG\r\n"))
		}
	}
}
	newConnPool := func() *pool.ConnPool {
		connPool := pool.NewConnPool(&pool.Options{
			Dialer:          dummyDialer,
			PoolSize:        poolSize,
			MinIdleConns:    minIdleConns,
			PoolTimeout:     100 * time.Millisecond,
			DialTimeout:     1 * time.Second,
			ConnMaxIdleTime: -1,
		})
		Eventually(func() int {
			return connPool.Len()
		}).Should(Equal(minIdleConns))
		return connPool
	}

	assert := func() {
		It("has idle connections when created", func() {
			Expect(connPool.Len()).To(Equal(minIdleConns))
			Expect(connPool.IdleLen()).To(Equal(minIdleConns))
		})

		Context("after Get", func() {
			var cn *pool.Conn

			BeforeEach(func() {
				var err error
				cn, err = connPool.Get(ctx)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() int {
					return connPool.Len()
				}).Should(Equal(minIdleConns + 1))
			})

			It("has idle connections", func() {
				Expect(connPool.Len()).To(Equal(minIdleConns + 1))
				Expect(connPool.IdleLen()).To(Equal(minIdleConns))
			})

			Context("after Remove", func() {
				BeforeEach(func() {
					connPool.Remove(ctx, cn, nil)
				})

				It("has idle connections", func() {
					Expect(connPool.Len()).To(Equal(minIdleConns))
					Expect(connPool.IdleLen()).To(Equal(minIdleConns))
				})
			})
		})

		Describe("Get does not exceed pool size", func() {
			var mu sync.RWMutex
			var cns []*pool.Conn

			BeforeEach(func() {
				cns = make([]*pool.Conn, 0)

				perform(poolSize, func(_ int) {
					defer GinkgoRecover()

					cn, err := connPool.Get(ctx)
					Expect(err).NotTo(HaveOccurred())
					mu.Lock()
					cns = append(cns, cn)
					mu.Unlock()
				})

				Eventually(func() int {
					return connPool.Len()
				}).Should(BeNumerically(">=", poolSize))
			})

			It("Get is blocked", func() {
				done := make(chan struct{})
				go func() {
					connPool.Get(ctx)
					close(done)
				}()

				select {
				case <-done:
					Fail("Get is not blocked")
				case <-time.After(time.Millisecond):
					// ok
				}

				select {
				case <-done:
					// ok
				case <-time.After(time.Second):
					Fail("Get is not unblocked")
				}
			})

			Context("after Put", func() {
				BeforeEach(func() {
					perform(len(cns), func(i int) {
						mu.RLock()
						connPool.Put(ctx, cns[i])
						mu.RUnlock()
					})

					Eventually(func() int {
						return connPool.Len()
					}).Should(Equal(poolSize))
				})

				It("pool.Len is back to normal", func() {
					Expect(connPool.Len()).To(Equal(poolSize))
					Expect(connPool.IdleLen()).To(Equal(poolSize))
				})
			})

			Context("after Remove", func() {
				BeforeEach(func() {
					perform(len(cns), func(i int) {
						mu.RLock()
						connPool.Remove(ctx, cns[i], nil)
						mu.RUnlock()
					})

					Eventually(func() int {
						return connPool.Len()
					}).Should(Equal(minIdleConns))
				})

				It("has idle connections", func() {
					Expect(connPool.Len()).To(Equal(minIdleConns))
					Expect(connPool.IdleLen()).To(Equal(minIdleConns))
				})
			})
		})
	}

	Context("minIdleConns = 1", func() {
		BeforeEach(func() {
			minIdleConns = 1
			connPool = newConnPool()
		})

		AfterEach(func() {
			connPool.Close()
		})

		assert()
	})

	Context("minIdleConns = 32", func() {
		BeforeEach(func() {
			minIdleConns = 32
			connPool = newConnPool()
		})

		AfterEach(func() {
			connPool.Close()
		})

		assert()
	})
})

var _ = Describe("race", func() {
	ctx := context.Background()
	var connPool *pool.ConnPool
	var C, N int

	BeforeEach(func() {
		C, N = 10, 1000
		if testing.Short() {
			C = 2
			N = 50
		}
	})

	AfterEach(func() {
		connPool.Close()
	})

	It("does not happen on Get, Put, and Remove", func() {
		connPool = pool.NewConnPool(&pool.Options{
			Dialer:          dummyDialer,
			PoolSize:        10,
			PoolTimeout:     time.Minute,
			DialTimeout:     1 * time.Second,
			ConnMaxIdleTime: time.Millisecond,
		})

		perform(C, func(id int) {
			for i := 0; i < N; i++ {
				cn, err := connPool.Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				if err == nil {
					connPool.Put(ctx, cn)
				}
			}
		}, func(id int) {
			for i := 0; i < N; i++ {
				cn, err := connPool.Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				if err == nil {
					connPool.Remove(ctx, cn, nil)
				}
			}
		})
	})

	It("limit the number of connections", func() {
		opt := &pool.Options{
			Dialer: func(ctx context.Context) (net.Conn, error) {
				return &net.TCPConn{}, nil
			},
			PoolSize:     1000,
			MinIdleConns: 50,
			PoolTimeout:  3 * time.Second,
			DialTimeout:  1 * time.Second,
		}
		p := pool.NewConnPool(opt)

		var wg sync.WaitGroup
		for i := 0; i < opt.PoolSize; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = p.Get(ctx)
			}()
		}
		wg.Wait()

		stats := p.Stats()
		Expect(stats.IdleConns).To(Equal(uint32(0)))
		Expect(stats.TotalConns).To(Equal(uint32(opt.PoolSize)))
	})

	It("wait", func() {
		opt := &pool.Options{
			Dialer: func(ctx context.Context) (net.Conn, error) {
				return &net.TCPConn{}, nil
			},
			PoolSize:    1,
			PoolTimeout: 3 * time.Second,
		}
		p := pool.NewConnPool(opt)

		wait := make(chan struct{})
		conn, _ := p.Get(ctx)
		go func() {
			_, _ = p.Get(ctx)
			wait <- struct{}{}
		}()
		time.Sleep(time.Second)
		p.Put(ctx, conn)
		<-wait

		stats := p.Stats()
		Expect(stats.IdleConns).To(Equal(uint32(0)))
		Expect(stats.TotalConns).To(Equal(uint32(1)))
		Expect(stats.WaitCount).To(Equal(uint32(1)))
		Expect(stats.WaitDurationNs).To(BeNumerically("~", time.Second.Nanoseconds(), 100*time.Millisecond.Nanoseconds()))
	})

	It("timeout", func() {
		testPoolTimeout := 1 * time.Second
		opt := &pool.Options{
			Dialer: func(ctx context.Context) (net.Conn, error) {
				// Artificial delay to force pool timeout
				time.Sleep(3 * testPoolTimeout)

				return &net.TCPConn{}, nil
			},
			PoolSize:    1,
			PoolTimeout: testPoolTimeout,
		}
		p := pool.NewConnPool(opt)

		stats := p.Stats()
		Expect(stats.Timeouts).To(Equal(uint32(0)))

		conn, err := p.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = p.Get(ctx)
		Expect(err).To(MatchError(pool.ErrPoolTimeout))
		p.Put(ctx, conn)
		conn, err = p.Get(ctx)
		Expect(err).NotTo(HaveOccurred())

		stats = p.Stats()
		Expect(stats.Timeouts).To(Equal(uint32(1)))
	})
})
