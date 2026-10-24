// Package minio implements an S3-compatible object storage connection.
package minio

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	_defaultConnAttempts = 10
	_defaultConnTimeout  = time.Second
	_defaultRegion       = "us-east-1"
)

// MinIO -.
type MinIO struct {
	connAttempts int
	connTimeout  time.Duration
	region       string
	secure       bool

	publicEndpoint string

	transport *http.Transport

	Client *miniogo.Client

	// presignClient builds the download links, and it points at the endpoint
	// the browser can reach. Inside a container the API endpoint and the link
	// host are not always the same, so the two clients are kept apart.
	presignClient *miniogo.Client

	buckets bucketClient
}

// New -.
func New(endpoint, accessKey, secretKey string, opts ...Option) (*MinIO, error) {
	m := &MinIO{
		connAttempts:   _defaultConnAttempts,
		connTimeout:    _defaultConnTimeout,
		region:         _defaultRegion,
		transport:      defaultTransport(),
		publicEndpoint: endpoint,
	}

	// Custom options
	for _, opt := range opts {
		opt(m)
	}

	client, err := newClient(endpoint, accessKey, secretKey, m)
	if err != nil {
		return nil, err
	}

	presignClient, err := newClient(m.publicEndpoint, accessKey, secretKey, m)
	if err != nil {
		return nil, err
	}

	m.Client = client
	m.presignClient = presignClient
	m.buckets = client

	err = m.check(context.Background())
	if err != nil {
		return nil, err
	}

	return m, nil
}

// newClient builds one SDK client pointed at an endpoint.
func newClient(endpoint, accessKey, secretKey string, m *MinIO) (*miniogo.Client, error) {
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:     credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:    m.secure,
		Region:    m.region,
		Transport: m.transport,
	})
	if err != nil {
		return nil, fmt.Errorf("minio - newClient - miniogo.New: %w", err)
	}

	return client, nil
}

// check lists the buckets, retrying until the attempts run out. The SDK has no
// ping, so the cheapest read call is used to prove the server is reachable.
func (m *MinIO) check(ctx context.Context) error {
	var err error

	for m.connAttempts > 0 {
		_, err = m.Client.ListBuckets(ctx)
		if err == nil {
			return nil
		}

		log.Printf("MinIO is trying to connect, attempts left: %d", m.connAttempts)

		time.Sleep(m.connTimeout)

		m.connAttempts--
	}

	return fmt.Errorf("minio - check - connAttempts == 0: %w", err)
}

// Close -.
func (m *MinIO) Close() {
	m.transport.CloseIdleConnections()
}

// defaultTransport returns a copy of the default transport so the pool can be
// drained without touching the shared one.
func defaultTransport() *http.Transport {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}

	return transport.Clone()
}
