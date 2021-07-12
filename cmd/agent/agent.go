package agent

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"

	"github.com/trento-project/trento/agent"
)

var TTL time.Duration
var port int
var consulConfigDir string
var UseEmbeddedConsul bool
var consulSrvAddr string
var consulBindAddr string

func NewAgentCmd() *cobra.Command {

	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Command tree related to the agent component",
	}

	runOnceCmd := &cobra.Command{
		Use:   "run-once",
		Short: "run-once",
		Run:   runOnce,
		Args:  startArgsValidator,
	}

	startCmd := &cobra.Command{
		Use:   "start path/to/definitions.yaml",
		Short: "Start the agent",
		Run:   start,
		Args:  startArgsValidator,
	}
	startCmd.Flags().DurationVar(&TTL, "ttl", time.Second*10, "Duration of Consul TTL checks")
	startCmd.Flags().IntVarP(&port, "port", "p", 8700, "The TCP port to use for the web service")
	startCmd.Flags().StringVarP(&consulConfigDir, "consul-config-dir", "", "consul.d", "Consul configuration directory used to store node meta-data")
	startCmd.Flags().BoolVar(&UseEmbeddedConsul, "use-embedded-consul", true, "Enable the usage of the trento embedded consul client")
	startCmd.Flags().StringVar(&consulSrvAddr, "consul-server-addr", "", "Consul server that the embedded client should connect to")
	startCmd.Flags().StringVar(&consulBindAddr, "consul-bind-addr", "0.0.0.0", "IP address that the embedded consul client should bind to")

	agentCmd.AddCommand(startCmd)
	agentCmd.AddCommand(runOnceCmd)

	return agentCmd
}

func runOnce(cmd *cobra.Command, args []string) {
	var err error

	checker, err := agent.NewChecker(args)
	if err != nil {
		log.Fatal("Failed to create a Checker instance: ", err)
	}

	res, err := checker()
	if err != nil {
		log.Fatal("Failed to do checks: ", err)
	}

	res.CheckPrettyPrint(os.Stdout)
}

func start(cmd *cobra.Command, args []string) {
	var err error

	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	cfg, err := agent.DefaultConfig()
	if err != nil {
		log.Fatal("Failed to create the agent configuration: ", err)
	}

	cfg.DefinitionsPaths = args
	cfg.WebPort = port
	cfg.CheckerTTL = TTL
	cfg.ConsulConfigDir = consulConfigDir
	cfg.UseEmbeddedConsul = UseEmbeddedConsul
	cfg.ConsulSrvAddr = net.ParseIP(consulSrvAddr)
	cfg.ConsulBindAddr = net.ParseIP(consulBindAddr)

	a, err := agent.NewWithConfig(cfg)
	if err != nil {
		log.Fatal("Failed to create the agent: ", err)
	}

	go func() {
		quit := <-signals
		log.Printf("Caught %s signal!", quit)

		log.Println("Stopping the agent...")
		a.Stop()
	}()

	log.Println("Starting the Console Agent...")
	err = a.Start()
	if err != nil {
		log.Fatal("Failed to start the agent: ", err)
	}
}

func startArgsValidator(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify at least one configuration yaml file")
	}

	for _, definitionsPath := range args {
		info, err := os.Lstat(definitionsPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("unable to find file %q", definitionsPath)
			}
			return fmt.Errorf("error when running os.Lstat(%q): %s", definitionsPath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("%q is a directory", definitionsPath)
		}
	}

	return nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
