package redis

import (
	"context"
)

// HealthCheckHook is called to determine if a connection is healthy.
// This can be used to implement custom health checks for scenarios like
// draining connections during node upgrades.
type HealthCheckHook interface {
	// Check returns nil if the connection is healthy.
	// Otherwise it returns an error describing the problem.
	Check(ctx context.Context, conn *Conn) error
}

// StreamingCredentialsProvider is an interface for streaming authentication credentials
type StreamingCredentialsProvider interface {
	// Subscribe registers a listener for credential updates
	Subscribe(listener CredentialsListener) (Credentials, UnsubscribeFunc, error)
}

// CredentialsListener is notified of credential changes
type CredentialsListener interface {
	// OnNext is called when new credentials are available
	OnNext(credentials Credentials)
	// OnError is called when an error occurs with credentials
	OnError(err error)
}

// Credentials interface represents authentication credentials
type Credentials interface {
	// BasicAuth returns username and password
	BasicAuth() (username string, password string)
	// RawCredentials returns the raw credential string
	RawCredentials() string
}

// UnsubscribeFunc is called to stop receiving credential updates
type UnsubscribeFunc func()

// ClusterSlotInfo represents a range of slots in a Redis cluster
type ClusterSlotInfo struct {
	Start int
	End   int
	Nodes []ClusterNodeInfo
}

// ClusterNodeInfo represents a node in a Redis cluster
type ClusterNodeInfo struct {
	Addr string
	ID   string
}

// Client types
const (
	// failoverClientType is already declared elsewhere in this package
	clusterClientType  = "cluster"
	singleClientType   = "single"
	sentinelClientType = "sentinel"
)

// Define other missing interfaces or types here
