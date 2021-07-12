package hosts

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	consulApi "github.com/hashicorp/consul/api"
	"github.com/pkg/errors"
	"github.com/rtorrero/bench-common/check"
	log "github.com/sirupsen/logrus"
	"github.com/trento-project/trento/internal"
	"github.com/trento-project/trento/internal/consul"
	"github.com/trento-project/trento/internal/sapsystem"
)

const TrentoPrefix string = "trento-"

type HostList []*Host

type Host struct {
	consulApi.Node
	client consul.Client
}

func NewHost(node consulApi.Node, client consul.Client) Host {
	return Host{node, client}
}

func (n *Host) Health() string {
	checks, _, _ := n.client.Health().Node(n.Name(), nil)
	return checks.AggregatedStatus()
}

func (n *Host) Name() string {
	return n.Node.Node
}

func (n *Host) TrentoMeta() map[string]string {
	filtered_meta := make(map[string]string)

	for key, value := range n.Node.Meta {
		if strings.HasPrefix(key, TrentoPrefix) {
			filtered_meta[key] = value
		}
	}
	return filtered_meta
}

// todo: this method was rushed, needs to be completely rewritten to have the checker webservice decoupled in a dedicated HTTP client
func (n *Host) HAChecks() *check.Controls {
	checks := &check.Controls{}

	var err error
	resp, err := http.Get(fmt.Sprintf("http://%s:%d", n.Address, 8700)) // todo: use a Consul service instead of directly addressing the node IP and port
	if err != nil {
		log.Print(err)
		return nil
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Print(err)
		return nil
	}

	err = json.Unmarshal(body, checks)
	if err != nil {
		log.Print(err)
		return nil
	}
	return checks
}

func (n *Host) GetSAPSystems() (map[string]*sapsystem.SAPSystem, error) {
	systems, err := sapsystem.Load(n.client, n.Name())
	if err != nil {
		return nil, err
	}
	return systems, nil
}

// Use github.com/hashicorp/go-bexpr to create the filter
// https://github.com/hashicorp/consul/blob/master/agent/consul/catalog_endpoint.go#L298
func CreateFilterMetaQuery(query map[string][]string) string {
	var filters []string
	// Need to sort the keys to have stable output. Mostly for unit testing
	sortedQuery := sortKeys(query)

	if len(query) != 0 {
		var filter string
		for _, key := range sortedQuery {
			if strings.HasPrefix(key, TrentoPrefix) {
				filter = ""
				values := query[key]
				for _, value := range values {
					filter = fmt.Sprintf("%sMeta[\"%s\"] == \"%s\"", filter, key, value)
					if values[len(values)-1] != value {
						filter = fmt.Sprintf("%s or ", filter)
					}
				}
				filters = append(filters, fmt.Sprintf("(%s)", filter))
			}
		}
	}
	return strings.Join(filters, " and ")
}

func Load(client consul.Client, query_filter string, health_filter []string) (HostList, error) {
	var hosts = HostList{}

	query := &consulApi.QueryOptions{Filter: query_filter}
	consul_nodes, _, err := client.Catalog().Nodes(query)
	if err != nil {
		return nil, errors.Wrap(err, "could not query Consul for nodes")
	}
	for _, node := range consul_nodes {
		populated_host := &Host{*node, client}
		// This check could be done in the frontend maybe
		if len(health_filter) == 0 || internal.Contains(health_filter, populated_host.Health()) {
			hosts = append(hosts, populated_host)
		}
	}

	return hosts, nil
}

func sortKeys(m map[string][]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
