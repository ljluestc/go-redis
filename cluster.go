package redis

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// ClusterOptions are used to configure a cluster client and should be
// passed to NewClusterClient.
type ClusterOptions struct {
	// A seed list of host:port addresses of cluster nodes.
	Addrs []string

	// ClientName will execute the `CLIENT SETNAME ClientName` command for each conn.
	ClientName string

	// NewClient creates a cluster node client with provided name and options.
	NewClient func(opt *Options) *Client

	// HealthCheckHook is called to determine if a connection is healthy.
	// This can be used to implement custom health checks for scenarios like
	// draining connections during node upgrades.
	// If not set, a default PING check will be used.
	HealthCheckHook HealthCheckHook

	// The maximum number of retries before giving up. Command is retried
	// on network errors and MOVED/ASK redirects.
	// Default is 3 retries.
	MaxRedirects int

	// Enables read-only commands on slave nodes.
	ReadOnly bool
	// Allows routing read-only commands to the closest master or slave node.
	// It automatically enables ReadOnly.
	RouteByLatency bool
	// Allows routing read-only commands to the random master or slave node.
	// It automatically enables ReadOnly.
	RouteRandomly bool

	// Enable routing read-only commands to slave nodes by using role.
	// It automatically enables ReadOnly.
	RouteByRole bool

	// Route all commands to slave read-only nodes.
	// It automatically enables ReadOnly.
	AlwaysSlaves bool

	// Optional function that returns cluster slots information.
	// It is useful to manually create cluster of standalone Redis servers
	// and load-balance read/write operations between slaves and masters.
	// It can use service like ZooKeeper to maintain configuration information
	// and state between them.
	ClusterSlots func(context.Context) ([]ClusterSlot, error)

	// Following options are copied from Options struct.

	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

	OnConnect func(ctx context.Context, cn *Conn) error

	Protocol int
	Username string
	Password string

	CredentialsProvider          func() (string, string)
	CredentialsProviderContext   func(ctx context.Context) (string, string, error)
	StreamingCredentialsProvider StreamingCredentialsProvider

	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration

	DialTimeout           time.Duration
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	ContextTimeoutEnabled bool

	// PoolFIFO uses FIFO mode for each node connection pool GET/PUT operations.
	// Default is false.
	PoolFIFO bool
	// PoolSize applies per cluster node and not for the whole cluster.
	PoolSize        int
	PoolTimeout     time.Duration
	MinIdleConns    int
	MaxIdleConns    int
	MaxActiveConns  int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration

	// TLSConfig for use by the dialer to establish a secure connection.
	TLSConfig *tls.Config

	// Limiter interface used to implement circuit breaker or rate limiter.
	Limiter Limiter

	DisableIndentity bool
	DisableIdentity  bool
	IdentityConfigs  map[string]string
	IdentitySuffix   string

	// If set, write operations are only performed to the master node.
	// If not set, they're performed to all masters in the cluster.
	// The default is to perform writes on all masters
	SingleMaster bool

	UnstableResp3 bool
}
