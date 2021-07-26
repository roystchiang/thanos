package memcache

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type ClusterConfig struct {
	version int
	nodes   []Node
}

type Node struct {
	dns  string
	ip   string
	port int
}

type Resolver interface {
	// Resolve performs a DNS lookup and returns a list of records.
	// name is the domain name to be resolved.
	// qtype is the query type. Accepted values are `dns` for A/AAAA lookup and `dnssrv` for SRV lookup.
	// If scheme is passed through name, it is preserved on IP results.
	Resolve(ctx context.Context, address string) (*ClusterConfig, error)
}

type memcachedAutoDiscovery struct {
	dialTimeout time.Duration
}

func (s *memcachedAutoDiscovery) Resolve(ctx context.Context, address string) (config *ClusterConfig, err error) {
	conn, err := net.DialTimeout("tcp", address, s.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = conn.Close()
	}()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if _, err := fmt.Fprintf(rw, "config get cluster\n"); err != nil {
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		return nil, err
	}

	config, err = s.parseConfig(rw.Reader)
	if err != nil {
		return nil, err
	}

	return config, err
}

func (s *memcachedAutoDiscovery) parseConfig(reader *bufio.Reader) (*ClusterConfig, error) {
	clusterConfig := new(ClusterConfig)

	configMeta, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read config metadata: %s", err)
	}
	configMeta = strings.TrimSpace(configMeta)

	// First line should be "CONFIG cluster 0 [length-of-payload-]
	configMetaComponents := strings.Split(configMeta, " ")
	if len(configMetaComponents) != 4 {
		return nil, fmt.Errorf("expected 4 components in config metadata, and recieved %d, meta: %s", len(configMetaComponents), configMeta)
	}

	configSize, err := strconv.Atoi(configMetaComponents[3])
	if err != nil {
		return nil, fmt.Errorf("failed to parse config size from metadata: %s, error: %s", configMeta, err)
	}

	configVersion, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to find config version: %s", err)
	}
	clusterConfig.version, err = strconv.Atoi(strings.TrimSpace(configVersion))

	nodes, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read nodes: %s", err)
	}

	if len(configVersion)+len(nodes) != configSize {
		return nil, fmt.Errorf("expected %d in config payload, but got %d instead.", configSize, len(configVersion)+len(nodes))
	}

	for _, host := range strings.Split(strings.TrimSpace(nodes), " ") {
		dnsIpPort := strings.Split(host, "|")
		if len(dnsIpPort) != 3 {
			return nil, fmt.Errorf("node not in expected format: %s", dnsIpPort)
		}
		port, err := strconv.Atoi(dnsIpPort[2])
		if err != nil {
			return nil, fmt.Errorf("failed to parse port: %s, err: %s", dnsIpPort, err)
		}
		clusterConfig.nodes = append(clusterConfig.nodes, Node{dns: dnsIpPort[0], ip: dnsIpPort[1], port: port})
	}

	return clusterConfig, nil
}
