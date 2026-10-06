package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

func newDeleteCommand(clientFn func() (client.Client, error)) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:          "delete <name>",
		Short:        "Delete an AdministrativeNetworkDomain and all its subnets",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if !yes {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "This will delete AND %q and all its associated resources. Continue? [y/N] ", name); err != nil {
					return err
				}
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				if strings.TrimSpace(strings.ToLower(answer)) != "y" {
					_, err = fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return err
				}
			}

			c, err := clientFn()
			if err != nil {
				return err
			}

			and := &v1beta1.AdministrativeNetworkDomain{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			}

			if err := c.Delete(context.TODO(), and); err != nil {
				return fmt.Errorf("deleting AND %q: %w", name, err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "administrativenetworkdomain/%s deleted\n", name)
			return err
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}
