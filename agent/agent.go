package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/consul-template/manager"
	consulAgent "github.com/hashicorp/consul/agent"
	consulApi "github.com/hashicorp/consul/api"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/trento-project/trento/agent/discovery"
	"github.com/trento-project/trento/internal/consul"
)

const haConfigCheckerId = "ha_config_checker"
const maxConsulRegisterRetries = 10

type Agent struct {
	cfg            Config
	check          Checker
	discoveries    []discovery.Discovery
	wsResultChan   chan CheckResult
	webService     *webService
	consul         consul.Client
	ctx            context.Context
	ctxCancel      context.CancelFunc
	templateRunner *manager.Runner
	consulAgent    *consulAgent.Agent
}

type Config struct {
	CheckerTTL        time.Duration
	DiscoveryTTL      time.Duration
	WebHost           string
	WebPort           int
	InstanceName      string
	DefinitionsPaths  []string
	ConsulConfigDir   string
	AgentHCLConfig    string
	UseEmbeddedConsul bool
	ConsulBindAddr    net.IP
	ConsulSrvAddr     net.IP
}

func New() (*Agent, error) {
	config, err := DefaultConfig()
	if err != nil {
		return nil, errors.Wrap(err, "could not create the agent configuration")
	}

	return NewWithConfig(config)
}

// returns a new instance of Agent with the given configuration
func NewWithConfig(cfg Config) (*Agent, error) {
	client, err := consul.DefaultClient()
	if err != nil {
		return nil, errors.Wrap(err, "could not create a Consul client")
	}

	checker, err := NewChecker(cfg.DefinitionsPaths)
	if err != nil {
		return nil, errors.Wrap(err, "could not create a Checker instance")
	}

	templateRunner, err := NewTemplateRunner(cfg.ConsulConfigDir)
	if err != nil {
		return nil, errors.Wrap(err, "could not create the consul template runner")
	}

	var consulAgent *consulAgent.Agent
	if cfg.UseEmbeddedConsul {

		consulAgent, err = NewConsulAgent(cfg.ConsulBindAddr, cfg.ConsulSrvAddr)
		if err != nil {
			return nil, errors.Wrap(err, "could not start embedded consul agent")
		}
	}

	ctx, ctxCancel := context.WithCancel(context.Background())

	wsResultChan := make(chan CheckResult, 1)

	agent := &Agent{
		cfg:       cfg,
		check:     checker,
		ctx:       ctx,
		ctxCancel: ctxCancel,
		consul:    client,
		discoveries: []discovery.Discovery{
			discovery.NewClusterDiscovery(client),
			discovery.NewSAPSystemsDiscovery(client),
			discovery.NewCloudDiscovery(client),
		},
		webService:     newWebService(wsResultChan),
		wsResultChan:   wsResultChan,
		templateRunner: templateRunner,
		consulAgent:    consulAgent,
	}
	return agent, nil
}

func DefaultConfig() (Config, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return Config{}, errors.Wrap(err, "could not read the hostname")
	}

	return Config{
		InstanceName: hostname,
		DiscoveryTTL: 2 * time.Minute,
		CheckerTTL:   10 * time.Second,
	}, nil
}

func DefaultAgentConfig() (Config, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return Config{}, errors.Wrap(err, "could not read the hostname")
	}

	return Config{
		InstanceName: hostname,
		DiscoveryTTL: 15 * time.Second,
		CheckerTTL:   10 * time.Second,
	}, nil
}

// Start the Agent which includes the registration against Consul Agent
func (a *Agent) Start() error {
	status := make(chan int, 1)
	var wg sync.WaitGroup

	// Attempt to register against the consul client
	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		log.Println("Registering the agent service with Consul...")
		defer wg.Done()
		err := a.startRegisterLoop()
		if err != nil {
			log.Fatal("Error while registering consul")
		} else {
			status <- 1
		}
		log.Println("Consul service registered.")
	}(&wg)

	if a.cfg.UseEmbeddedConsul {
		wg.Add(1)
		// Attempt to start the embedded consul agent
		go func(wg *sync.WaitGroup) {
			log.Println("Starting embedded consul agent...")
			defer wg.Done()
			err := a.startConsulAgent(status)
			if err != nil {
				log.Println("An error ocurred while trying to start Consul: ", err)
				return
			}
		}(&wg)
	}

	wg.Add(1)
	// The Checker Loop is handling the compliance-checks being executed regularly
	// and reporting a Service Status (WARN/FAIL)
	go func(wg *sync.WaitGroup) {
		log.Println("Starting Check loop...")
		defer wg.Done()
		a.startCheckTicker()
		log.Println("Check loop stopped.")
	}(&wg)

	wg.Add(1)
	// The Discover Loop is executing at a much slower pace than the Checker Loop
	// and will keep namespaces in Key-Value Consul store updated with specific facts
	// discovered on the node
	go func(wg *sync.WaitGroup) {
		log.Println("Starting Discover loop...")
		defer wg.Done()
		a.startDiscoverTicker()
		log.Println("Discover loop stopped.")
	}(&wg)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		err := a.webService.Start(a.cfg.WebHost, a.cfg.WebPort, a.ctx)
		if err != nil {
			log.Println("Error while starting the Agent web service:", err)
		}
	}(&wg)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		log.Println("Starting consul-template loop...")
		defer wg.Done()
		a.startConsulTemplate()
		log.Println("consul-template loop stopped.")
	}(&wg)

	wg.Wait()

	return nil
}

func (a *Agent) Stop() {
	a.ctxCancel()
}

func (a *Agent) registerConsulService() error {
	var err error

	consulService := &consulApi.AgentServiceRegistration{
		ID:   a.cfg.InstanceName,
		Name: "trento-agent",
		Tags: []string{"trento"},
		Checks: consulApi.AgentServiceChecks{
			&consulApi.AgentServiceCheck{
				CheckID: haConfigCheckerId,
				Name:    "HA Config Checker",
				Notes:   "Checks whether or not the HA configuration is compliant with the provided best practices",
				TTL:     a.cfg.CheckerTTL.String(),
				Status:  consulApi.HealthWarning,
			},
			&consulApi.AgentServiceCheck{
				CheckID: discovery.ClusterDiscoveryId,
				Name:    "HA Cluster Discovery",
				Notes:   "Collects details about the HA Cluster components running on this node",
				TTL:     a.cfg.DiscoveryTTL.String(),
				Status:  consulApi.HealthWarning,
			},
			&consulApi.AgentServiceCheck{
				CheckID: discovery.SAPDiscoveryId,
				Name:    "SAP System Discovery",
				Notes:   "Collects details about SAP System components running on this node",
				TTL:     a.cfg.DiscoveryTTL.String(),
				Status:  consulApi.HealthWarning,
			},
			&consulApi.AgentServiceCheck{
				CheckID: discovery.CloudDiscoveryId,
				Name:    "Cloud metadata discovery",
				Notes:   "Collects details about the cloud instance metadata",
				TTL:     a.cfg.DiscoveryTTL.String(),
				Status:  consulApi.HealthWarning,
			},
		},
	}

	err = a.consul.Agent().ServiceRegister(consulService)
	if err != nil {
		return errors.Wrap(err, "could not register the agent service with Consul")
	}

	return nil
}

func (a *Agent) startRegisterLoop() error {
	attempts := 0
	for {
		err := a.registerConsulService()
		if (err != nil) && (attempts < maxConsulRegisterRetries) {
			log.Println("An error ocurred while attempting to register:", err)
			time.Sleep(3 * time.Second)
			attempts++
			continue
		}

		return err
	}
}

func (a *Agent) startCheckTicker() {
	tick := func() {
		result, err := a.check()
		if err != nil {
			log.Println("An error occurred while running health checks:", err)
			return
		}
		a.wsResultChan <- result
		a.updateConsulCheck(result)
	}
	defer close(a.wsResultChan)

	interval := a.cfg.CheckerTTL / 2

	repeat(tick, interval, a.ctx)
}

// Start a Ticker loop that will iterate over the hardcoded list of Discovery backends
// and execute them. The initial run will happen relatively quickly after Agent launch
// subsequent runs are done with a 15 minute delay. The effectiveness of the discoveries
// is reported back in the "discover_cluster" Service in consul under a TTL of 60 minutes
func (a *Agent) startDiscoverTicker() {
	tick := func() {
		for _, d := range a.discoveries {
			var status string
			var err error

			err = d.Discover()
			if err != nil {
				log.Printf("Error while running discovery '%s': %s", d.GetId(), err)
				status = consulApi.HealthWarning
			} else {
				status = consulApi.HealthPassing
			}

			err = a.consul.Agent().UpdateTTL(d.GetId(), "", status)
			if err != nil {
				log.Println("An error occurred while trying to update TTL with Consul:", err)
			}
		}
	}

	interval := a.cfg.DiscoveryTTL / 2

	repeat(tick, interval, a.ctx)
}

func (a *Agent) updateConsulCheck(result CheckResult) {
	log.Println("Updating Consul check TTL...")

	var err error
	var status string

	summary := result.Summary()

	switch {
	case summary.Fail > 0:
		status = consulApi.HealthCritical
	case summary.Warn > 0:
		status = consulApi.HealthWarning
	default:
		status = consulApi.HealthPassing
	}

	checkOutput := fmt.Sprintf("%s\nFor more detailed info, query this service at port %d.",
		result.String(), a.cfg.WebPort)
	err = a.consul.Agent().UpdateTTL(haConfigCheckerId, checkOutput, status)
	if err != nil {
		log.Println("An error occurred while trying to update TTL with Consul:", err)
		return
	}

	log.Printf("Consul check TTL updated. Status: %s.", status)
}

func repeat(tick func(), interval time.Duration, ctx context.Context) {
	// run the first tick immediately
	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tick()
		case <-ctx.Done():
			return
		}
	}
}
