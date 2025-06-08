package redis

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// FailoverOptions are used to configure a failover client and should
// be passed to NewFailoverClient.
type FailoverOptions struct {
	// The master name.
	MasterName string

	// A seed list of host:port addresses of sentinel nodes.
	SentinelAddrs []string

	// The sentinel password.
	SentinelPassword string

	// The sentinel username.
	SentinelUsername string

	// FailoverTimeout for Sentinel failover.
	FailoverTimeout time.Duration

	// Route all commands to slave read-only nodes.
	SlaveOnly bool

	// Enable routing read-only commands to slave nodes.
	ReplicaOnly bool

	// Route commands by latency to the closest node.
	RouteByLatency bool

	// Route commands to random node.
	RouteRandomly bool

	// Use replicas disconnected with master when cannot get connected replicas
	// Now, this option only works in RandomReplicaAddr function.
	UseDisconnectedReplicas bool

	// HealthCheckHook is called to determine if a connection is healthy.
	// This can be used to implement custom health checks for scenarios like
	// draining connections during node upgrades.
	// If not set, a default PING check will be used.
	HealthCheckHook HealthCheckHook

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

	PoolFIFO        bool
	PoolSize        int
	PoolTimeout     time.Duration
	MinIdleConns    int
	MaxIdleConns    int
	MaxActiveConns  int
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration

	TLSConfig *tls.Config

	Limiter Limiter

	DisableIndentity bool
	DisableIdentity  bool
	IdentityConfigs  map[string]string
	IdentitySuffix   string

	DB           int
	MaxRedirects int

	// Only cluster clients.

	ClusterSlots func(context.Context) ([]ClusterSlot, error)

	// The maximum number of redirects before giving up.
	// Command is retried on network errors and MOVED/ASK redirects.
	// Default is 3 redirects.
	// NOTE: If there are no slaves in the cluster, the value of this field will be used instead of MaxRetries.
	//       This is because the client will retry commands on the same node.
	//       So if the master is down, the client will retry the command MaxRedirects times.
	//       If there are slaves in the cluster, the client will retry the command MaxRetries times on different nodes.
	//       So if the master is down, and there is at least one slave, the client will retry the command MaxRetries times.

	ReadOnly bool

	// Enables routing read-only commands to the closest slave node.
	RouteByRole bool

	// Enables routing all commands to slave read-only nodes.
	AlwaysSlaves bool

	// SingleMaster flag. If set, only one master is used from the cluster.
	SingleMaster bool

	UnstableResp3 bool

	// ClientName will execute the `CLIENT SETNAME ClientName` command for each conn.
	ClientName string
}
