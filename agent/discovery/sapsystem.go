package discovery

import (
	"github.com/trento-project/trento/internal/consul"
	"github.com/trento-project/trento/internal/environments"
	"github.com/trento-project/trento/internal/hosts"
	"github.com/trento-project/trento/internal/sapsystem"
)

const SAPDiscoveryId string = "sap_system_discovery"

type SAPSystemsDiscovery struct {
	id         string
	discovery  BaseDiscovery
	SAPSystems sapsystem.SAPSystemsList
}

func NewSAPSystemsDiscovery(client consul.Client) SAPSystemsDiscovery {
	r := SAPSystemsDiscovery{}
	r.id = SAPDiscoveryId
	r.discovery = NewDiscovery(client)
	return r
}

func (d SAPSystemsDiscovery) GetId() string {
	return d.id
}

func (d SAPSystemsDiscovery) Discover() (string, error) {
	systems, err := sapsystem.NewSAPSystemsList()

	if err != nil {
		return "", err
	}

	d.SAPSystems = systems
	for _, s := range d.SAPSystems {
		err := s.Store(d.discovery.client)
		if err != nil {
			return "", err
		}

		// Store SAP System, Landscape and Environment names on hosts metadata
		err = storeSAPSystemTags(d.discovery.client, s)
		if err != nil {
			return "", err
		}
	}

	return "", nil
}

func storeSAPSystemTags(client consul.Client, system *sapsystem.SAPSystem) error {
	envName, landName, sysName, err := loadSAPSystemTags(client, system.SID)
	if err != nil {
		return err
	}

	// If we didn't find any environment, we create a new default one
	if envName == "" {
		land := environments.NewDefaultLandscape()
		land.AddSystem(environments.NewSystem(sysName, system.Type))
		env := environments.NewDefaultEnvironment()
		env.AddLandscape(land)

		err := env.Store(client)
		if err != nil {
			return err
		}
		envName = env.Name
		landName = land.Name
	}

	// Store host metadata
	metadata := hosts.Metadata{
		Environment: envName,
		Landscape:   landName,
		SAPSystem:   sysName,
	}

	err = metadata.Store(client)
	if err != nil {
		return err
	}

	return nil
}

// These methods must go here. We cannot put them in the internal/sapsystem.go package
// as this creates potential cyclical imports
func loadSAPSystemTags(client consul.Client, sid string) (string, string, string, error) {
	var env, land string
	sys := sid

	envs, err := environments.Load(client)
	if err != nil {
		return env, land, sys, err
	}
	for envKey, envValue := range envs {
		for landKey, landValue := range envValue.Landscapes {
			for sysKey := range landValue.SAPSystems {
				if sysKey == sys {
					env = envKey
					land = landKey
					break
				}
			}
		}
	}
	return env, land, sys, err
}
