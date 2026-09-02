// nolint:goconst,prealloc
package replica_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	"github.com/openmcp-project/multicluster-provider/pkg/provider"
	"github.com/openmcp-project/multicluster-provider/pkg/testing/fake"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/api/install"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/replica"
)

// defaultTestSetup sets up a test environment with a fake multicluster provider and a ReplicaController.
// The initial state is loaded from the directory specified by the path segments. The directory is expected to follow this structure:
// - 'platform' folder (optional, but likely always present): Contains all manifests which should be applied to the platform cluster.
// - 'onboarding' folder (optional): Contains all manifests which should be applied to the onboarding cluster.
// - All other folders are expected to follow the naming convention 'cluster_<namespace>_<name>' and contain all manifests which should be applied to the cluster with the given namespace and name.
//   - This causes the test to fail if the platform cluster does not contain a Cluster resource with the given namespace and name.
//
// Note that both, the platform and the onboarding cluster, will have corresponding Cluster resources created by default, in the openmcp-system namespace, with the name 'platform' and 'onboarding', respectively.
func defaultTestSetup(testDataDirPathSegments ...string) (*testutils.ComplexEnvironment, multicluster.Provider, *replica.ReplicaController) {
	GinkgoHelper()
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())

	envb := testutils.NewComplexEnvironmentBuilder().WithFakeClient(platformCluster, scheme).WithFakeClient(onboardingCluster, scheme)

	clusterPrefix := "cluster_"

	// default Cluster resources for the platform and onboarding clusters
	defaultClusterResources := []client.Object{
		&clustersv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      platformCluster,
				Namespace: "openmcp-system",
			},
			Spec: clustersv1alpha1.ClusterSpec{
				Profile:  "test",
				Purposes: []string{clustersv1alpha1.PURPOSE_PLATFORM},
				Tenancy:  clustersv1alpha1.TENANCY_SHARED,
			},
		},
		&clustersv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      onboardingCluster,
				Namespace: "openmcp-system",
			},
			Spec: clustersv1alpha1.ClusterSpec{
				Profile:  "test",
				Purposes: []string{clustersv1alpha1.PURPOSE_ONBOARDING},
				Tenancy:  clustersv1alpha1.TENANCY_SHARED,
			},
		},
	}
	envb.WithInitObjects(platformCluster, defaultClusterResources...)

	testDataDir := filepath.Join(testDataDirPathSegments...)

	dirs, err := os.ReadDir(testDataDir)
	Expect(err).ToNot(HaveOccurred())

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		name := dir.Name()
		dirPath := filepath.Join(testDataDir, name)
		if name == platformCluster || name == onboardingCluster {
			envb.WithInitObjectPath(name, dirPath)
		} else if clusterSuffix, ok := strings.CutPrefix(name, clusterPrefix); ok {
			// directory name is cluster_<namespace>_<name>; split at the first underscore of the suffix
			parts := strings.SplitN(clusterSuffix, "_", 2)
			if len(parts) != 2 {
				Fail("cluster directory name does not follow the 'cluster_<namespace>_<name>' convention: " + name)
			}
			cName := provider.ClusterName(parts[0], parts[1])
			envb.WithFakeClient(string(cName), scheme)
			envb.WithInitObjectPath(string(cName), dirPath)
		}
	}

	envb.WithDynamicObjectsWithStatus(platformCluster, &repv1alpha1.Replica{}, &repv1alpha1.ClusterReplica{})
	env := envb.Build()

	prov := fake.NewProvider()
	for name, cl := range env.Clusters {
		cName := multicluster.ClusterName(name)
		if name == platformCluster {
			cName = provider.HostingPlatformCluster
		}
		Expect(prov.Add(env.Ctx, cName, fake.NewCluster(scheme, fake.WithClient(cl)))).To(Succeed())
	}

	ctrl := replica.NewReplicaController(prov, providerName, nil)

	return env, prov, ctrl
}

func findConditionWithPrefix(conditions []metav1.Condition, prefix string) (metav1.Condition, bool) {
	for _, c := range conditions {
		if strings.HasPrefix(c.Type, prefix) {
			return c, true
		}
	}
	return metav1.Condition{}, false
}

var _ = Describe("Replica Controller", func() {

	Context("TargetConflictPolicy", func() {

		It("should fail when the target already exists and the policy is 'Fail'", func() {
			env, _, ctrl := defaultTestSetup("testdata", "test-01")

			rep := &repv1alpha1.Replica{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-replica-fail"}, rep)).To(Succeed())

			_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
			Expect(err).To(HaveOccurred())
			var reasonableErr errutils.ReasonableError
			Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
			Expect(reasonableErr.Reason()).To(Equal(cconst.ReasonConfigurationProblem))
			Expect(reasonableErr.Error()).To(ContainSubstring("already exists and is not owned by this (Cluster)Replica"))

			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(rep), rep)).To(Succeed())
			conflictCondition, found := findConditionWithPrefix(rep.Status.Conditions, repv1alpha1.ConditionTypeTargetPrefix)
			Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
			Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(conflictCondition.Reason).To(Equal(cconst.ReasonConfigurationProblem))
		})

		It("should skip the target without error when it already exists and the policy is 'Skip'", func() {
			env, _, ctrl := defaultTestSetup("testdata", "test-01")

			rep := &repv1alpha1.Replica{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-replica-skip"}, rep)).To(Succeed())

			_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
			Expect(err).ToNot(HaveOccurred())

			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(rep), rep)).To(Succeed())
			skipCondition, found := findConditionWithPrefix(rep.Status.Conditions, repv1alpha1.ConditionTypeTargetPrefix)
			Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
			Expect(skipCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(skipCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSkipped))

			existing := &corev1.Secret{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, existing)).To(Succeed())
			Expect(existing.Data).To(HaveKey("other"), "pre-existing target should not have been modified")
			Expect(existing.Data).ToNot(HaveKey("key"), "pre-existing target should not have been modified")
		})

		It("should overwrite the target without error when it already exists and the policy is 'Overwrite'", func() {
			env, _, ctrl := defaultTestSetup("testdata", "test-01")

			rep := &repv1alpha1.Replica{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-replica-overwrite"}, rep)).To(Succeed())

			_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
			Expect(err).ToNot(HaveOccurred())

			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(rep), rep)).To(Succeed())
			overwriteCondition, found := findConditionWithPrefix(rep.Status.Conditions, repv1alpha1.ConditionTypeTargetPrefix)
			Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
			Expect(overwriteCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(overwriteCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSynced))

			overwritten := &corev1.Secret{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, overwritten)).To(Succeed())
			Expect(overwritten.Data).To(HaveKey("key"), "target should have been overwritten with source data")
			Expect(overwritten.Data).ToNot(HaveKey("other"), "target should have been overwritten with source data")
			Expect(overwritten.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, providerName))
			Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceKindLabel, strings.ToLower(repv1alpha1.KindReplica)))
			Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceNameLabel, rep.Name))
			Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceNamespaceLabel, rep.Namespace))

			Expect(rep.Status.Replicas).To(ContainElement(repv1alpha1.CreatedResourcesWithType{
				Type: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
				Resources: []repv1alpha1.CreatedResource{
					{ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{Name: "source-secret", Namespace: "target-ns"}},
				},
			}))
		})

		It("should fail when the target already exists, but is in deletion, and the policy is 'Overwrite'", func() {
			env, _, ctrl := defaultTestSetup("testdata", "test-01")

			preexisting := &corev1.Secret{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, preexisting)).To(Succeed())
			controllerutil.AddFinalizer(preexisting, "test/block-deletion")
			Expect(env.Client(platformCluster).Update(env.Ctx, preexisting)).To(Succeed())
			Expect(env.Client(platformCluster).Delete(env.Ctx, preexisting)).To(Succeed())

			rep := &repv1alpha1.Replica{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-replica-overwrite-in-deletion"}, rep)).To(Succeed())

			_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
			Expect(err).To(HaveOccurred())
			var reasonableErr errutils.ReasonableError
			Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
			Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonTargetConflict))
			Expect(reasonableErr.Error()).To(ContainSubstring("is in deletion"))

			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(rep), rep)).To(Succeed())
			conflictCondition, found := findConditionWithPrefix(rep.Status.Conditions, repv1alpha1.ConditionTypeTargetPrefix)
			Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
			Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(conflictCondition.Reason).To(Equal(repv1alpha1.ReasonTargetConflict))
		})

		It("should fail when the target already exists, but is owned by another Replica, and the policy is 'Overwrite'", func() {
			env, _, ctrl := defaultTestSetup("testdata", "test-01")

			rep := &repv1alpha1.Replica{}
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-replica-overwrite-owned-by-other"}, rep)).To(Succeed())

			_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
			Expect(err).To(HaveOccurred())
			var reasonableErr errutils.ReasonableError
			Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
			Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonTargetConflict))
			Expect(reasonableErr.Error()).To(ContainSubstring("owned by other"))

			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(rep), rep)).To(Succeed())
			conflictCondition, found := findConditionWithPrefix(rep.Status.Conditions, repv1alpha1.ConditionTypeTargetPrefix)
			Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
			Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(conflictCondition.Reason).To(Equal(repv1alpha1.ReasonTargetConflict))
		})

	})

})
