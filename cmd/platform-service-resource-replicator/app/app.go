package app

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	"github.com/openmcp-project/platform-service-resource-replicator/api/install"
)

func NewResourceReplicatorCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-service-resource-replicator",
		Short: "Commands for interacting with the platform-service-resource-replicator",
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	so := &SharedOptions{
		RawSharedOptions: &RawSharedOptions{
			PlatformCluster: clusters.New("platform"),
		},
	}

	so.AddPersistentFlags(cmd)
	cmd.AddCommand(NewInitCommand(so))
	cmd.AddCommand(NewRunCommand(so))

	return cmd
}

type RawSharedOptions struct {
	Environment string `json:"environment"`
	DryRun      bool   `json:"dry-run"`

	PlatformCluster *clusters.Cluster `json:"platform-cluster"`
	ProviderName    string            `json:"provider-name"`
}

type SharedOptions struct {
	*RawSharedOptions

	// fields filled in Complete()
	Log logging.Logger
}

func (o *SharedOptions) AddPersistentFlags(cmd *cobra.Command) {
	// logging
	logging.InitFlags(cmd.PersistentFlags())
	// misc
	cmd.PersistentFlags().BoolVar(&o.DryRun, "dry-run", false, "If set, the command aborts after evaluation of the given flags.")
	cmd.PersistentFlags().StringVar(&o.Environment, "environment", "", "Environment name. Required. This is used to distinguish between different environments that are watching the same Onboarding cluster. Must be globally unique.")
	cmd.PersistentFlags().StringVar(&o.ProviderName, "provider-name", "", "Name of the provider resource")

	o.PlatformCluster.RegisterSingleConfigPathFlag(cmd.PersistentFlags())
}

func (o *SharedOptions) Complete() error {
	// platform cluster
	if err := o.PlatformCluster.InitializeRESTConfig(); err != nil {
		return fmt.Errorf("unable to initialize platform cluster rest config: %w", err)
	}
	platformScheme := install.InstallOperatorAPIsPlatform(install.InstallCRDAPIs(runtime.NewScheme()))

	if err := o.PlatformCluster.InitializeClient(platformScheme); err != nil {
		return fmt.Errorf("unable to initialize platform cluster client: %w", err)
	}

	// build logger
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(o.Log.Logr())

	return nil
}

func (o *SharedOptions) PrintRaw(cmd *cobra.Command) {
	data, err := yaml.Marshal(o.RawSharedOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *SharedOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{
		"platform-cluster": o.PlatformCluster.APIServerEndpoint(),
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}
