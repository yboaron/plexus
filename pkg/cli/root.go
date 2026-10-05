package cli

import (
	"github.com/spf13/cobra"
)

var (
	kubeconfig  string
	kubecontext string
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plexus",
		Short: "Plexus — manage AdministrativeNetworkDomains",
		Long: `Plexus is a network orchestrator that provides VPC-like isolation
on Kubernetes. Use this CLI to create, manage, and inspect
AdministrativeNetworkDomains (ANDs) and their subnets.

This command can be used standalone or as a kubectl plugin (kubectl plexus).`,
	}

	cmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	cmd.PersistentFlags().StringVar(&kubecontext, "context", "", "The name of the kubeconfig context to use")

	cmd.AddCommand(newCreateCommand(getClient))
	cmd.AddCommand(newDeleteCommand(getClient))
	cmd.AddCommand(newDescribeCommand(getClient))
	cmd.AddCommand(newAddSubnetCommand(getClient))
	cmd.AddCommand(newDeleteSubnetCommand(getClient))
	cmd.AddCommand(newVersionCommand())

	return cmd
}
