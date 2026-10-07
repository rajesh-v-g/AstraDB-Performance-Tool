package driver

import (
	"archive/zip"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gocql/gocql"
)

// SessionParams holds all parameters required to create a CQL session.
type SessionParams struct {
	// Driver is "astra" or "cassandra".
	Driver string
	// Astra fields.
	Token   string
	SCBPath string
	// Cassandra fields.
	Hosts    []string
	Port     int
	Username string
	Password string
	// Shared fields.
	Keyspace         string
	ConsistencyLevel string
}

// NewSession creates a new gocql.Session based on params.Driver.
// The returned Executor is a *gocql.Session which satisfies the Executor interface.
func NewSession(params SessionParams) (Executor, error) {
	switch params.Driver {
	case "astra":
		return newAstraSession(params)
	default:
		return newCassandraSession(params)
	}
}

// astraConfig is the subset of config.json inside an Astra SCB zip that we need.
type astraConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	CQLPort  int    `json:"cql_port"`
	Keyspace string `json:"keyspace"`
	LocalDC  string `json:"localDC"` // Astra SCB uses "localDC", not "localDCID"
}

// newAstraSession opens the SCB zip, extracts TLS material + connection metadata,
// and creates a gocql session using standard SslOptions — no driver fork needed.
func newAstraSession(params SessionParams) (*gocql.Session, error) {
	if params.SCBPath == "" {
		return nil, fmt.Errorf("astra: SCB path is required")
	}

	// Read all files from the SCB zip into memory.
	files, err := readZip(params.SCBPath)
	if err != nil {
		return nil, fmt.Errorf("astra: open SCB %q: %w", params.SCBPath, err)
	}

	// Parse config.json for host / port.
	cfgBytes, ok := files["config.json"]
	if !ok {
		return nil, fmt.Errorf("astra: SCB missing config.json")
	}
	var cfg astraConfig
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, fmt.Errorf("astra: parse config.json: %w", err)
	}

	// Extract TLS material.
	caCert, hasCa := files["ca.crt"]
	certPem, hasCert := files["cert"]
	keyPem, hasKey := files["key"]
	if !hasCa || !hasCert || !hasKey {
		return nil, fmt.Errorf("astra: SCB missing TLS files (ca.crt / cert / key)")
	}

	// Build the TLS config from the in-memory cert bytes.
	tlsCfg, err := buildTLSConfig(caCert, certPem, keyPem)
	if err != nil {
		return nil, fmt.Errorf("astra: build TLS config: %w", err)
	}

	// Determine connection target.
	host := cfg.Host
	port := cfg.CQLPort
	if port == 0 {
		port = cfg.Port
	}
	if port == 0 {
		port = 29042 // Astra default CQL port
	}
	if host == "" {
		return nil, fmt.Errorf("astra: config.json has no host")
	}

	// SNI: ServerName must match the Astra hostname so the TLS handshake is
	// routed to the correct database tenant.
	tlsCfg.ServerName = host

	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.SslOpts = &gocql.SslOptions{
		Config:                 tlsCfg,
		EnableHostVerification: false, // server identity is verified via CA pool + ServerName above
	}
	cluster.Authenticator = gocql.PasswordAuthenticator{
		Username: "token",
		Password: params.Token,
	}

	// DC-aware routing is required for Astra; without it gocql may attempt
	// connections to nodes in other DCs and fail with "no connections were made".
	if cfg.LocalDC != "" {
		cluster.PoolConfig.HostSelectionPolicy =
			gocql.DCAwareRoundRobinPolicy(cfg.LocalDC)
	}

	keyspace := params.Keyspace
	if keyspace == "" {
		keyspace = cfg.Keyspace
	}
	if keyspace != "" {
		cluster.Keyspace = keyspace
	}
	cluster.Consistency = parseConsistency(params.ConsistencyLevel)
	cluster.Timeout = 15 * time.Second
	cluster.ConnectTimeout = 20 * time.Second
	cluster.NumConns = 2

	return cluster.CreateSession()
}

// newCassandraSession creates a session pointing at a vanilla Cassandra cluster.
func newCassandraSession(params SessionParams) (*gocql.Session, error) {
	hosts := params.Hosts
	if len(hosts) == 0 {
		hosts = []string{"localhost"}
	}
	cluster := gocql.NewCluster(hosts...)
	if params.Port > 0 {
		cluster.Port = params.Port
	}
	if params.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: params.Username,
			Password: params.Password,
		}
	}
	if params.Keyspace != "" {
		cluster.Keyspace = params.Keyspace
	}
	cluster.Consistency = parseConsistency(params.ConsistencyLevel)
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 15 * time.Second
	cluster.NumConns = 2

	return cluster.CreateSession()
}

// parseConsistency maps a consistency level string to the gocql constant.
func parseConsistency(s string) gocql.Consistency {
	switch s {
	case "LOCAL_ONE":
		return gocql.LocalOne
	case "LOCAL_QUORUM", "LOCAL_SERIAL":
		return gocql.LocalQuorum
	case "ALL":
		return gocql.All
	default:
		return gocql.LocalQuorum
	}
}

// readZip reads all files in a zip archive into a map[filename]bytes.
func readZip(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	r, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, err
	}

	out := make(map[string][]byte, len(r.File))
	for _, zf := range r.File {
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("open %q in zip: %w", zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %q in zip: %w", zf.Name, err)
		}
		out[zf.Name] = data
	}
	return out, nil
}

// buildTLSConfig constructs a *tls.Config from PEM-encoded cert material.
func buildTLSConfig(caCert, certPem, keyPem []byte) (*tls.Config, error) {
	// CA pool.
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// Client certificate.
	clientCert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, fmt.Errorf("parse client cert/key: %w", err)
	}

	return &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{clientCert},
		// Astra uses SNI-based routing; InsecureSkipVerify is NOT set —
		// the CA pool above validates the server certificate chain.
		MinVersion: tls.VersionTLS12,
	}, nil
}
