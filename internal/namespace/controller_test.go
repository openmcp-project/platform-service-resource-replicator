// nolint:goconst
package namespace_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	"github.com/openmcp-project/multicluster-provider/pkg/provider"
	"github.com/openmcp-project/multicluster-provider/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/api/install"
	nsctrl "github.com/openmcp-project/platform-service-resource-replicator/internal/namespace"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/shared"
)

const (
	providerClusterName = "ns-a/cluster-a"
)

// defaultTestSetup initializes a new environment for testing the NamespaceController.
// It returns:
//   - ctx: the test context
//   - platformClient: client for the platform cluster (holds Replica/ClusterReplica resources)
//   - ctrl: the NamespaceController under test
//   - providerClusterClient: client for the fake provider cluster (for namespace lookups on remote clusters)
func defaultTestSetup(testDirPathSegments ...string) (context.Context, client.Client, *nsctrl.NamespaceController, client.Client) {
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())

	// Load init objects from testdata so they are pre-populated in the platform fake client.
	initObjects := []client.Object{} // nolint:prealloc
	if len(testDirPathSegments) > 0 {
		var err error
		initObjects, err = testutils.LoadObjects(filepath.Join(testDirPathSegments...), scheme)
		if err != nil {
			panic(err)
		}
	}

	platformCluster := fake.NewCluster(scheme, fake.WithClientBuilder(
		fakeclient.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(initObjects...).
			WithStatusSubresource(append(initObjects, &repv1alpha1.Replica{}, &repv1alpha1.ClusterReplica{})...),
	))
	providerCluster := fake.NewCluster(scheme)

	prov := fake.NewProvider()
	ctx := testutils.NewComplexEnvironmentBuilder().Build().Ctx
	if err := prov.Add(ctx, provider.HostingPlatformCluster, platformCluster); err != nil {
		panic(err)
	}
	if err := prov.Add(ctx, multicluster.ClusterName(providerClusterName), providerCluster); err != nil {
		panic(err)
	}

	ctrl := nsctrl.New(prov)

	return ctx, platformCluster.GetClient(), ctrl, providerCluster.GetClient()
}

// reconcileNamespace calls the namespace controller's Reconcile for the given namespace and cluster.
func reconcileNamespace(ctrl *nsctrl.NamespaceController, ctx context.Context, clusterName multicluster.ClusterName, nsName string) {
	GinkgoHelper()
	req := mcreconcile.Request{
		Request:     testutils.RequestFromStrings(nsName),
		ClusterName: clusterName,
	}
	_, err := ctrl.Reconcile(ctx, req)
	Expect(err).ToNot(HaveOccurred())
}

// drainQueue drains the shared notification channel and returns keys of enqueued replicas.
// Format: "<namespace>/<name>" for Replica, "<name>" for ClusterReplica.
func drainQueue() []string {
	ch := shared.SharedInformation().GetReplicaNotificationChannel()
	var enqueued []string
	for {
		select {
		case evt := <-ch:
			key := evt.Object.GetName()
			if ns := evt.Object.GetNamespace(); ns != "" {
				key = ns + "/" + key
			}
			enqueued = append(enqueued, key)
		default:
			return enqueued
		}
	}
}

// createNamespace creates a Namespace on the given client.
func createNamespace(ctx context.Context, cl client.Client, name string, labels map[string]string) {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	Expect(cl.Create(ctx, ns)).To(Succeed())
}

var _ = Describe("NamespaceController", Serial, func() {

	BeforeEach(func() {
		shared.SharedInformation().Reset()
	})

	It("enqueues Replicas and ClusterReplicas matching by selector or status on the platform cluster, and skips non-matching ones", func() {
		ctx, platformClient, ctrl, _ := defaultTestSetup("testdata", "test-01")

		// target-ns matches the "env=test" selector used by rep-selector-match and crep-selector-match.
		// rep-status-match and crep-status-match have a status entry for "target-ns" on the platform cluster.
		// rep-nomatch and crep-nomatch use "env=prod" which does not match.
		createNamespace(ctx, platformClient, "target-ns", map[string]string{"env": "test"})
		reconcileNamespace(ctrl, ctx, provider.HostingPlatformCluster, "target-ns")

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-selector-match",
			"crep-selector-match",
			"default/rep-status-match",
			"crep-status-match",
		))
	})

	It("enqueues Replicas and ClusterReplicas by status when labels no longer match the selector", func() {
		ctx, platformClient, ctrl, _ := defaultTestSetup("testdata", "test-01")

		// target-ns does NOT have "env=test", so selector-based replicas are not triggered.
		// rep-status-match and crep-status-match still have a status entry for "target-ns".
		createNamespace(ctx, platformClient, "target-ns", map[string]string{"env": "something-else"})
		reconcileNamespace(ctrl, ctx, provider.HostingPlatformCluster, "target-ns")

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-status-match",
			"crep-status-match",
		))
	})

	It("enqueues Replicas and ClusterReplicas whose status refers to a namespace on a provider cluster", func() {
		ctx, _, ctrl, providerClient := defaultTestSetup("testdata", "test-02")

		// target-ns exists on the provider cluster (ns-a/cluster-a).
		// rep-status-prov-match and crep-status-prov-match have a status entry for "target-ns" on cluster-a/ns-a.
		// rep-nomatch and crep-nomatch reference cluster-b/ns-b, which does not match.
		createNamespace(ctx, providerClient, "target-ns", nil)
		reconcileNamespace(ctrl, ctx, multicluster.ClusterName(providerClusterName), "target-ns")

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-status-prov-match",
			"crep-status-prov-match",
		))
	})

	It("does not enqueue anything when no replica matches the namespace event", func() {
		ctx, platformClient, ctrl, _ := defaultTestSetup("testdata", "test-01")

		// other-ns has no labels that match any selector, and no replica has a status entry for it.
		createNamespace(ctx, platformClient, "other-ns", nil)
		reconcileNamespace(ctrl, ctx, provider.HostingPlatformCluster, "other-ns")

		Expect(drainQueue()).To(BeEmpty())
	})
})
