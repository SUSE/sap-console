package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/trento-project/trento/internal/cluster"
	"github.com/trento-project/trento/internal/consul"
	"github.com/trento-project/trento/internal/hosts"
)

type Node struct {
	Name       string
	Attributes map[string]string
	Resources  []*Resource
	Ip         string
	Health     string
	VirtualIps []string
}

type Resource struct {
	Id        string
	Type      string
	Role      string
	Status    string
	FailCount int
}

type Nodes []*Node

func stoppedResources(c *cluster.Cluster) []*Resource {
	var stoppedResources []*Resource

	for _, r := range c.Crmmon.Resources {
		if r.NodesRunningOn == 0 && !r.Active {
			resource := &Resource{
				Id: r.Id,
			}
			stoppedResources = append(stoppedResources, resource)
		}
	}

	return stoppedResources
}

func (node *Node) Role() string {
	if r, ok := node.Attributes["hana_prd_roles"]; ok {
		role := r[strings.LastIndex(r, ":")+1:]
		return strings.Title(role)
	}
	return "-"
}

func (node *Node) HealthState() string {
	if r, ok := node.Attributes["hana_prd_roles"]; ok {
		healthState := strings.SplitN(r, ":", 2)[0]
		return healthState
	}
	return "-"
}

func (node *Node) Status() string {
	if r, ok := node.Attributes["hana_prd_roles"]; ok {
		status := strings.SplitN(r, ":", 3)[1]

		switch status {
		case "P":
			return "Primary"
		case "S":
			return "Secondary"
		}
	}
	return "-"
}

func NewNodes(c *cluster.Cluster, hl hosts.HostList) Nodes {
	var nodes Nodes

	for _, n := range c.Crmmon.NodeAttributes.Nodes {
		node := &Node{Name: n.Name, Attributes: make(map[string]string)}

		for _, a := range n.Attributes {
			node.Attributes[a.Name] = a.Value
		}

		for _, r := range c.Crmmon.Resources {
			if r.Node.Name == n.Name {
				resource := &Resource{
					Id:   r.Id,
					Type: r.Agent,
					Role: r.Role,
				}

				for _, p := range c.Cib.Configuration.Resources.Primitives {
					if r.Agent == "ocf::heartbeat:IPaddr2" && r.Id == p.Id {
						node.VirtualIps = append(node.VirtualIps, p.InstanceAttributes[0].Value)
						break
					}
					switch {
					case r.Active:
						resource.Status = "active"
					case r.Blocked:
						resource.Status = "blocked"
					case r.Failed:
						resource.Status = "failed"
					case r.FailureIgnored:
						resource.Status = "failure_ignored"
					case r.Orphaned:
						resource.Status = "orphaned"
					}
				}

				for _, nh := range c.Crmmon.NodeHistory.Nodes {
					if nh.Name == n.Name {
						for _, rh := range nh.ResourceHistory {
							if rh.Name == resource.Id {
								resource.FailCount = rh.FailCount
								break
							}
						}
					}
				}

				node.Resources = append(node.Resources, resource)
			}
		}

		for _, h := range hl {
			if h.Name() == node.Name {
				node.Ip = h.Address
				node.Health = h.Health()
			}
		}

		nodes = append(nodes, node)
	}

	return nodes
}

func (nodes Nodes) SecondarySyncState() string {
	for _, n := range nodes {
		if n.Status() == "Secondary" {
			if s, ok := n.Attributes["hana_prd_sync_state"]; ok {
				return s
			}
		}
	}
	return "-"
}

func (nodes Nodes) GroupBySite() map[string]Nodes {
	nodesBySite := make(map[string]Nodes)

	for _, n := range nodes {
		if site, ok := n.Attributes["hana_prd_site"]; ok {
			nodesBySite[site] = append(nodesBySite[site], n)
		}
	}

	return nodesBySite
}

func (nodes Nodes) CriticalCount() int {
	var critical int

	for _, n := range nodes {
		if n.Health == "failed" || n.Health == "maintenance" || n.Health == "" {
			critical += 1
		}
	}

	return critical
}

func (nodes Nodes) WarningCount() int {
	var warning int

	for _, n := range nodes {
		if n.Health == "warning" {
			warning += 1
		}
	}

	return warning
}

func NewClusterListHandler(client consul.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		clusters, err := cluster.Load(client)
		if err != nil {
			_ = c.Error(err)
			return
		}

		c.HTML(http.StatusOK, "clusters.html.tmpl", gin.H{
			"Clusters": clusters,
		})
	}
}

func NewClusterHandler(client consul.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		clusterId := c.Param("id")

		clusters, err := cluster.Load(client)
		if err != nil {
			_ = c.Error(err)
			return
		}

		cluster, ok := clusters[clusterId]
		if !ok {
			_ = c.Error(NotFoundError("could not find cluster"))
			return
		}

		filterQuery := fmt.Sprintf("Meta[\"trento-ha-cluster-id\"] == \"%s\"", clusterId)
		hosts, err := hosts.Load(client, filterQuery, nil)
		if err != nil {
			_ = c.Error(err)
			return
		}

		c.HTML(http.StatusOK, "cluster.html.tmpl", gin.H{
			"Cluster":          cluster,
			"Nodes":            NewNodes(cluster, hosts),
			"StoppedResources": stoppedResources(cluster),
		})
	}
}
