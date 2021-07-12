package discovery

import (
	"os"

	"github.com/trento-project/trento/internal/consul"
)

type Discovery interface {
	// Returns an arbitrary unique string identifier of the discovery, so that we can associate it to a Consul check ID
	GetId() string
	// Execute the discovery mechanism
	Discover() (string, error)
}

type BaseDiscovery struct {
	id     string
	client consul.Client
	host   string
}

func (d BaseDiscovery) GetId() string {
	return d.id
}

// Execute one iteration of a discovery and store the result in the Consul KVStore.
func (d BaseDiscovery) Discover() (string, error) {
	d.host, _ = os.Hostname()
	return "", nil
}

// Return a Host Discover instance
func NewDiscovery(client consul.Client) BaseDiscovery {
	r := BaseDiscovery{}
	r.id = ""
	r.client = client
	r.host, _ = os.Hostname()
	return r
}
