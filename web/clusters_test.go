package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	consulApi "github.com/hashicorp/consul/api"

	"github.com/stretchr/testify/assert"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/html"
	"github.com/trento-project/trento/internal/consul"
	"github.com/trento-project/trento/internal/consul/mocks"
)

func clustersListMap() map[string]interface{} {
	listMap := map[string]interface{}{
		"47d1190ffb4f781974c8356d7f863b03": map[string]interface{}{
			"cib": map[string]interface{}{
				"Configuration": map[string]interface{}{
					"CrmConfig": map[string]interface{}{
						"ClusterProperties": []interface{}{
							map[string]interface{}{
								"Id":    "cib-bootstrap-options-cluster-name",
								"Value": "test_cluster",
							},
						},
					},
				},
			},
			"crmmon": map[string]interface{}{
				"NodeAttributes": map[string]interface{}{
					"Nodes": []interface{}{
						map[string]interface{}{
							"Name": "test_node_1",
							"Attributes": []interface{}{
								map[string]interface{}{
									"Name":  "hana_prd_srmode",
									"Value": "sync",
								},
								map[string]interface{}{
									"Name":  "hana_prd_op_mode",
									"Value": "logreplay",
								},
								map[string]interface{}{
									"Name":  "hana_prd_roles",
									"Value": "4:P:master1:master:worker:master",
								},
								map[string]interface{}{
									"Name":  "hana_prd_sync_state",
									"Value": "PRIM",
								},
								map[string]interface{}{
									"Name":  "hana_prd_site",
									"Value": "site1",
								},
							},
						},
						map[string]interface{}{
							"Name": "test_node_2",
							"Attributes": []interface{}{
								map[string]interface{}{
									"Name":  "hana_prd_srmode",
									"Value": "sync",
								},
								map[string]interface{}{
									"Name":  "hana_prd_op_mode",
									"Value": "logreplay",
								},
								map[string]interface{}{
									"Name":  "hana_prd_roles",
									"Value": "4:S:master1:master:worker:master",
								},
								map[string]interface{}{
									"Name":  "hana_prd_sync_state",
									"Value": "SFAIL",
								},
								map[string]interface{}{
									"Name":  "hana_prd_site",
									"Value": "site2",
								},
							},
						},
					},
				},
				"Summary": map[string]interface{}{
					"Nodes": map[string]interface{}{
						"Number": 3,
					},
					"LastChange": map[string]interface{}{
						"Time": "Wed Jun 30 18:11:37 2021",
					},
					"Resources": map[string]interface{}{
						"Number": 5,
					},
				},
				"Resources": []interface{}{
					map[string]interface{}{
						"Id":    "sbd",
						"Agent": "stonith:external/sbd",
						"Role":  "started",
						"Node": map[string]interface{}{
							"Name": "test_node_1",
						},
					},
				},
			},
			"name": "sculpin",
		},
		"e2f2eb50aef748e586a7baa85e0162cf": map[string]interface{}{
			"cib": map[string]interface{}{
				"Configuration": map[string]interface{}{
					"CrmConfig": map[string]interface{}{
						"ClusterProperties": []interface{}{
							map[string]interface{}{
								"Id":    "cib-bootstrap-options-cluster-name",
								"Value": "2nd_cluster",
							},
						},
					},
				},
			},
			"crmmon": map[string]interface{}{
				"Summary": map[string]interface{}{
					"Nodes": map[string]interface{}{
						"Number": 2,
					},
					"Resources": map[string]interface{}{
						"Number": 10,
					},
				},
			},
			"name": "panther",
		},
	}

	return listMap
}

func TestClustersListHandler(t *testing.T) {
	consulInst := new(mocks.Client)
	kv := new(mocks.KV)

	consulInst.On("KV").Return(kv)

	kv.On("ListMap", consul.KvClustersPath, consul.KvClustersPath).Return(clustersListMap(), nil)
	consulInst.On("WaitLock", consul.KvClustersPath).Return(nil)

	deps := DefaultDependencies()
	deps.consul = consulInst

	var err error
	app, err := NewAppWithDeps("", 80, deps)
	if err != nil {
		t.Fatal(err)
	}

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/clusters", nil)
	if err != nil {
		t.Fatal(err)
	}

	app.ServeHTTP(resp, req)

	consulInst.AssertExpectations(t)
	kv.AssertExpectations(t)

	m := minify.New()
	m.AddFunc("text/html", html.Minify)
	m.Add("text/html", &html.Minifier{
		KeepDefaultAttrVals: true,
		KeepEndTags:         true,
	})
	minified, err := m.String("text/html", resp.Body.String())
	if err != nil {
		panic(err)
	}

	assert.Equal(t, 200, resp.Code)
	assert.Contains(t, minified, "Clusters")
	assert.Regexp(t, regexp.MustCompile("<td>sculpin</td><td></td><td>3</td><td>5</td><td>.*passing.*</td>"), minified)
	assert.Regexp(t, regexp.MustCompile("<td>panther</td><td></td><td>2</td><td>10</td><td>.*passing.*</td>"), minified)
}

func TestClusterHandler(t *testing.T) {
	nodes := []*consulApi.Node{
		{
			Node:    "test_node_1",
			Address: "192.168.1.1",
		},
		{
			Node:    "test_node_2",
			Address: "192.168.1.2",
		},
	}

	node1HealthChecks := consulApi.HealthChecks{
		&consulApi.HealthCheck{
			Status: consulApi.HealthPassing,
		},
	}

	node2HealthChecks := consulApi.HealthChecks{
		&consulApi.HealthCheck{
			Status: consulApi.HealthCritical,
		},
	}

	consulInst := new(mocks.Client)

	kv := new(mocks.KV)
	consulInst.On("KV").Return(kv)
	kv.On("ListMap", consul.KvClustersPath, consul.KvClustersPath).Return(clustersListMap(), nil)
	consulInst.On("WaitLock", consul.KvClustersPath).Return(nil)

	catalog := new(mocks.Catalog)
	filter := &consulApi.QueryOptions{Filter: "Meta[\"trento-ha-cluster-id\"] == \"47d1190ffb4f781974c8356d7f863b03\""}
	catalog.On("Nodes", filter).Return(nodes, nil, nil)
	consulInst.On("Catalog").Return(catalog)

	health := new(mocks.Health)
	health.On("Node", "test_node_1", (*consulApi.QueryOptions)(nil)).Return(node1HealthChecks, nil, nil)
	health.On("Node", "test_node_2", (*consulApi.QueryOptions)(nil)).Return(node2HealthChecks, nil, nil)
	consulInst.On("Health").Return(health)

	deps := DefaultDependencies()
	deps.consul = consulInst

	app, err := NewAppWithDeps("", 80, deps)
	if err != nil {
		t.Fatal(err)
	}

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/clusters/47d1190ffb4f781974c8356d7f863b03", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")

	app.ServeHTTP(resp, req)

	m := minify.New()
	m.AddFunc("text/html", html.Minify)
	m.Add("text/html", &html.Minifier{
		KeepDefaultAttrVals: true,
		KeepEndTags:         true,
	})
	minified, err := m.String("text/html", resp.Body.String())
	if err != nil {
		panic(err)
	}

	assert.NoError(t, err)
	assert.Equal(t, 200, resp.Code)
	assert.Contains(t, resp.Body.String(), "Cluster details")
	assert.Regexp(t, regexp.MustCompile("<strong>Cluster name:</strong><br><span.*>sculpin</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<strong>HANA system replication mode:</strong><br><span.*>sync</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<strong>Stonith type:</strong><br><span.*>external/sbd</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<strong>HANA system replication operation mode:</strong><br><span.*>logreplay</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<strong>CIB last written:</strong><br><span.*>Wed Jun 30 18:11:37 2021</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<strong>HANA secondary sync state:</strong><br><span.*>SFAIL</span>"), minified)
	assert.Regexp(t, regexp.MustCompile("<td>test_node_1</td><td>192.168.1.1</td>"), minified)
	assert.Regexp(t, regexp.MustCompile("<td>test_node_2</td><td>192.168.1.2</td>"), minified)
}

func TestClusterHandler404Error(t *testing.T) {
	var err error

	kv := new(mocks.KV)
	kv.On("ListMap", consul.KvClustersPath, consul.KvClustersPath).Return(clustersListMap(), nil)

	consulInst := new(mocks.Client)
	consulInst.On("KV").Return(kv)
	consulInst.On("WaitLock", consul.KvClustersPath).Return(nil)

	deps := DefaultDependencies()
	deps.consul = consulInst

	app, err := NewAppWithDeps("", 80, deps)
	if err != nil {
		t.Fatal(err)
	}

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/clusters/foobar", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")

	app.ServeHTTP(resp, req)

	assert.NoError(t, err)
	assert.Equal(t, 404, resp.Code)
	assert.Contains(t, resp.Body.String(), "Not Found")
}
