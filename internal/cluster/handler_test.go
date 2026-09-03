// nolint:goconst
package cluster_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/api/install"
	clusterhandler "github.com/openmcp-project/platform-service-resource-replicator/internal/cluster"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/shared"
)

// defaultTestSetup loads testdata into a fake platform client and returns a ClusterHandler with no handler-level selector.
func defaultTestSetup(testDirPathSegments ...string) (context.Context, *clusterhandler.ClusterHandler, *fakeclient.ClientBuilder) {
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())

	initObjects := []client.Object{} // nolint:prealloc
	if len(testDirPathSegments) > 0 {
		var err error
		initObjects, err = testutils.LoadObjects(filepath.Join(testDirPathSegments...), scheme)
		if err != nil {
			panic(err)
		}
	}

	builder := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjects...).
		WithStatusSubresource(append(initObjects, &repv1alpha1.Replica{}, &repv1alpha1.ClusterReplica{})...)

	ctx := testutils.NewComplexEnvironmentBuilder().Build().Ctx
	handler := clusterhandler.New(nil)
	return ctx, handler, builder
}

// makeCluster creates a minimal Cluster object with the given namespace, name and labels.
// nolint:unparam
func makeCluster(namespace, name string, labels map[string]string) *clustersv1alpha1.Cluster {
	return &clustersv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
		Spec: clustersv1alpha1.ClusterSpec{
			Profile:  "test",
			Tenancy:  clustersv1alpha1.TENANCY_EXCLUSIVE,
			Purposes: []string{"test"},
		},
	}
}

// invokeHandler calls HandleCreateOrUpdate on the handler with the given platform client and cluster.
func invokeHandler(handler *clusterhandler.ClusterHandler, ctx context.Context, platformClient client.Client, clusterObj *clustersv1alpha1.Cluster) {
	GinkgoHelper()
	req := mcreconcile.Request{}
	_, err := handler.HandleCreateOrUpdate(ctx, req, platformClient, clusterObj, nil)
	Expect(err).ToNot(HaveOccurred())
}

// invokeDeleteHandler calls HandleDelete on the handler.
func invokeDeleteHandler(handler *clusterhandler.ClusterHandler, ctx context.Context, platformClient client.Client, clusterObj *clustersv1alpha1.Cluster) {
	GinkgoHelper()
	req := mcreconcile.Request{}
	_, err := handler.HandleDelete(ctx, req, platformClient, clusterObj, nil)
	Expect(err).ToNot(HaveOccurred())
}

// drainQueue drains the shared notification channel and returns "<namespace>/<name>" keys for Replicas, "<name>" for ClusterReplicas.
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

var _ = Describe("ClusterHandler", Serial, func() {

	BeforeEach(func() {
		shared.SharedInformation().Reset()
	})

	It("enqueues Replicas and ClusterReplicas with an empty or matching cluster selector on HandleCreateOrUpdate", func() {
		ctx, handler, builder := defaultTestSetup("testdata", "test-01")
		platformClient := builder.Build()
		cl := makeCluster("ns-a", "cluster-a", map[string]string{"env": "test"})

		// rep-selector-match and crep-selector-match have 'cluster: {}' (empty selector → matches all).
		// rep-label-selector-match and crep-label-selector-match select 'env=test' which matches.
		// rep-nomatch and crep-nomatch select 'env=prod' which does not match.
		invokeHandler(handler, ctx, platformClient, cl)

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-selector-match",
			"crep-selector-match",
			"default/rep-label-selector-match",
			"crep-label-selector-match",
		))
	})

	It("enqueues Replicas and ClusterReplicas by status on HandleCreateOrUpdate", func() {
		ctx, handler, builder := defaultTestSetup("testdata", "test-02")
		platformClient := builder.Build()
		cl := makeCluster("ns-a", "cluster-a", nil)

		// rep-status-match and crep-status-match have a status entry referencing ns-a/cluster-a.
		// rep-nomatch and crep-nomatch reference ns-b/cluster-b.
		invokeHandler(handler, ctx, platformClient, cl)

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-status-match",
			"crep-status-match",
		))
	})

	It("enqueues Replicas and ClusterReplicas on HandleDelete just like on HandleCreateOrUpdate", func() {
		ctx, handler, builder := defaultTestSetup("testdata", "test-02")
		platformClient := builder.Build()
		cl := makeCluster("ns-a", "cluster-a", nil)

		invokeDeleteHandler(handler, ctx, platformClient, cl)

		Expect(drainQueue()).To(ConsistOf(
			"default/rep-status-match",
			"crep-status-match",
		))
	})

	It("skips Replicas and ClusterReplicas annotated with the ignore annotation", func() {
		ctx, handler, builder := defaultTestSetup("testdata", "test-03")
		platformClient := builder.Build()
		cl := makeCluster("ns-a", "cluster-a", nil)

		invokeHandler(handler, ctx, platformClient, cl)

		Expect(drainQueue()).To(BeEmpty())
	})

	It("IsResponsibleFor returns false when the handler-level selector does not match the cluster", func() {
		ctx := testutils.NewComplexEnvironmentBuilder().Build().Ctx
		sel := &clustersv1alpha1.IdentityLabelPurposeSelector{
			LabelSelector: clustersv1alpha1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
		}
		handler := clusterhandler.New(sel)
		cl := makeCluster("ns-a", "cluster-a", map[string]string{"env": "test"})

		Expect(handler.IsResponsibleFor(ctx, mcreconcile.Request{}, nil, cl)).To(BeFalse())
	})

	It("IsResponsibleFor returns true when the handler-level selector matches the cluster", func() {
		ctx := testutils.NewComplexEnvironmentBuilder().Build().Ctx
		sel := &clustersv1alpha1.IdentityLabelPurposeSelector{
			LabelSelector: clustersv1alpha1.LabelSelector{
				MatchLabels: map[string]string{"env": "test"},
			},
		}
		handler := clusterhandler.New(sel)
		cl := makeCluster("ns-a", "cluster-a", map[string]string{"env": "test"})

		Expect(handler.IsResponsibleFor(ctx, mcreconcile.Request{}, nil, cl)).To(BeTrue())
	})

	It("IsResponsibleFor returns true when the handler-level selector is nil", func() {
		ctx := testutils.NewComplexEnvironmentBuilder().Build().Ctx
		handler := clusterhandler.New(nil)
		cl := makeCluster("ns-a", "cluster-a", map[string]string{"env": "anything"})

		Expect(handler.IsResponsibleFor(ctx, mcreconcile.Request{}, nil, cl)).To(BeTrue())
	})
})
