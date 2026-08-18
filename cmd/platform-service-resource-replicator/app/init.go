package app

import (
	"context"
	"fmt"

	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/spf13/cobra"

	"github.com/openmcp-project/platform-service-resource-replicator/api/crds"
)

func NewInitCommand(so *SharedOptions) *cobra.Command {
	opts := &InitOptions{
		SharedOptions: so,
	}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the platform-service-resource-replicator",
		Run: func(cmd *cobra.Command, args []string) {
			if err := opts.Complete(cmd.Context()); err != nil {
				panic(fmt.Errorf("error completing options: %w", err))
			}
			opts.PrintCompletedOptions(cmd)
			if opts.DryRun {
				cmd.Println("=== END OF DRY RUN ===")
				return
			}
			if err := opts.Run(cmd.Context()); err != nil {
				panic(err)
			}
		},
	}
	opts.AddFlags(cmd)

	return cmd
}

type InitOptions struct {
	*SharedOptions
}

func (o *InitOptions) AddFlags(cmd *cobra.Command) {
}

func (o *InitOptions) Complete(ctx context.Context) error {
	if err := o.SharedOptions.Complete(); err != nil {
		return err
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	log := o.Log.WithName("main")
	log.Info("Environment", "value", o.Environment)

	// apply CRDs
	crdManager := crdutil.NewCRDManager(apiconst.ClusterLabel, crds.CRDs)

	crdManager.AddCRDLabelToClusterMapping("platform", o.PlatformCluster)

	if err := crdManager.CreateOrUpdateCRDs(ctx, &log); err != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", err)
	}

	log.Info("Finished init command")
	return nil
}

func (o *InitOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *InitOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
