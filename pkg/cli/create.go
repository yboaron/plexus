package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

func newCreateCommand(clientFn func() (client.Client, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create an AdministrativeNetworkDomain",
		Long: `Create an empty AdministrativeNetworkDomain.
Use 'plexus add-subnet' to add subnets after creation.

Examples:
  plexus create production
  plexus create staging`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := clientFn()
			if err != nil {
				return err
			}

			and := &v1beta1.AdministrativeNetworkDomain{
				ObjectMeta: metav1.ObjectMeta{
					Name: args[0],
				},
			}

			if err := c.Create(context.TODO(), and); err != nil {
				return fmt.Errorf("creating AND %q: %w", args[0], err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "administrativenetworkdomain/%s created\n", args[0])
			return err
		},
	}
}
