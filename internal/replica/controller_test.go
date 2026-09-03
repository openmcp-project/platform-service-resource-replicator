// nolint:goconst,prealloc,unparam
package replica_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
// Unless withClusterReplicas is false, a ClusterReplica is automatically created for every Replica resource found in the initial state,
// with the name '<namespace>-<name>', an identical spec, and the same finalizers, labels, annotations, and status.
func defaultTestSetup(withClusterReplicas bool, testDataDirPathSegments ...string) (*testutils.ComplexEnvironment, multicluster.Provider, *replica.ReplicaController) {
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

	if withClusterReplicas {
		reps := &repv1alpha1.ReplicaList{}
		Expect(env.Client(platformCluster).List(env.Ctx, reps)).To(Succeed())
		for _, rep := range reps.Items {
			cr := &repv1alpha1.ClusterReplica{
				ObjectMeta: metav1.ObjectMeta{
					Name:        rep.Namespace + "-" + rep.Name,
					Labels:      rep.Labels,
					Annotations: rep.Annotations,
					Finalizers:  rep.Finalizers,
				},
				Spec:   *rep.Spec.DeepCopy(),
				Status: *rep.Status.DeepCopy(),
			}
			Expect(env.Client(platformCluster).Create(env.Ctx, cr)).To(Succeed())
			if len(cr.Status.Conditions) > 0 || len(cr.Status.Replicas) > 0 {
				Expect(env.Client(platformCluster).Status().Update(env.Ctx, cr)).To(Succeed())
			}
		}
	}

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

// getReplicaEquivalent fetches a Replica or ClusterReplica resource by name and kind.
// For Replica resources, the namespace is used; for ClusterReplica resources, it is ignored
// and the name is constructed as '<namespace>-<name>'.
func getReplicaEquivalent(env *testutils.ComplexEnvironment, namespace, name, kind string) repv1alpha1.ReplicaEquivalent {
	GinkgoHelper()
	rep, err := getReplicaEquivalentWithError(env, namespace, name, kind)
	Expect(err).ToNot(HaveOccurred(), "failed to get %s %s/%s: %v", kind, namespace, name, err)
	return rep
}

func getReplicaEquivalentWithError(env *testutils.ComplexEnvironment, namespace, name, kind string) (repv1alpha1.ReplicaEquivalent, error) {
	GinkgoHelper()
	if kind == repv1alpha1.KindReplica {
		rep := &repv1alpha1.Replica{}
		err := env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: namespace, Name: name}, rep)
		return rep, err
	}
	cr := &repv1alpha1.ClusterReplica{}
	err := env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Name: namespace + "-" + name}, cr)
	return cr, err
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

	for _, replicaKind := range []string{repv1alpha1.KindReplica, repv1alpha1.KindClusterReplica} {
		withClusterReplicas := replicaKind == repv1alpha1.KindClusterReplica

		Context(replicaKind, func() {

			Context("TargetConflictPolicy", func() {

				It("should fail when the target already exists and the policy is 'Fail'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-01")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-fail", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).To(HaveOccurred())
					var reasonableErr errutils.ReasonableError
					Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
					Expect(reasonableErr.Reason()).To(Equal(cconst.ReasonConfigurationProblem))
					Expect(reasonableErr.Error()).To(ContainSubstring("already exists and is not owned by this (Cluster)Replica"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-fail", replicaKind)
					conflictCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(conflictCondition.Reason).To(Equal(cconst.ReasonConfigurationProblem))
				})

				It("should skip the target without error when it already exists and the policy is 'Skip'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-01")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-skip", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-skip", replicaKind)
					skipCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(skipCondition.Status).To(Equal(metav1.ConditionTrue))
					Expect(skipCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSkipped))

					existing := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, existing)).To(Succeed())
					Expect(existing.Data).To(HaveKey("other"), "pre-existing target should not have been modified")
					Expect(existing.Data).ToNot(HaveKey("key"), "pre-existing target should not have been modified")
				})

				It("should overwrite the target without error when it already exists and the policy is 'Overwrite'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-01")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-overwrite", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-overwrite", replicaKind)
					overwriteCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(overwriteCondition.Status).To(Equal(metav1.ConditionTrue))
					Expect(overwriteCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSynced))

					overwritten := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, overwritten)).To(Succeed())
					Expect(overwritten.Data).To(HaveKey("key"), "target should have been overwritten with source data")
					Expect(overwritten.Data).ToNot(HaveKey("other"), "target should have been overwritten with source data")
					Expect(overwritten.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, providerName))
					Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceKindLabel, strings.ToLower(replicaKind)))
					Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceNameLabel, rep.GetName()))
					if replicaKind == repv1alpha1.KindReplica {
						Expect(overwritten.Labels).To(HaveKeyWithValue(repv1alpha1.ReplicaSourceNamespaceLabel, rep.GetNamespace()))
					} else {
						Expect(overwritten.Labels).ToNot(HaveKey(repv1alpha1.ReplicaSourceNamespaceLabel))
					}

					Expect(rep.GetStatus().Replicas).To(ContainElement(repv1alpha1.CreatedResourcesWithType{
						Type: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
						Resources: []repv1alpha1.CreatedResource{
							{ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{Name: "source-secret", Namespace: "target-ns"}},
						},
					}))
				})

				It("should fail when the target already exists (in deletion) and the policy is 'Overwrite'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-01")

					preexisting := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, preexisting)).To(Succeed())
					controllerutil.AddFinalizer(preexisting, "test/block-deletion")
					Expect(env.Client(platformCluster).Update(env.Ctx, preexisting)).To(Succeed())
					Expect(env.Client(platformCluster).Delete(env.Ctx, preexisting)).To(Succeed())

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-overwrite-in-deletion", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).To(HaveOccurred())
					var reasonableErr errutils.ReasonableError
					Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
					Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonTargetConflict))
					Expect(reasonableErr.Error()).To(ContainSubstring("is in deletion"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-overwrite-in-deletion", replicaKind)
					conflictCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(conflictCondition.Reason).To(Equal(repv1alpha1.ReasonTargetConflict))
				})

				It("should fail when the target already exists (owned by another Replica) and the policy is 'Overwrite'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-01")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-overwrite-owned-by-other", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).To(HaveOccurred())
					var reasonableErr errutils.ReasonableError
					Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
					Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonTargetConflict))
					Expect(reasonableErr.Error()).To(ContainSubstring("owned by other"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-overwrite-owned-by-other", replicaKind)
					conflictCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(conflictCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(conflictCondition.Reason).To(Equal(repv1alpha1.ReasonTargetConflict))
				})

			})

			Context("NamespacePolicy", func() {

				It("should fail when the target namespace does not exist and the policy is 'Fail'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-02")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-ns-fail", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).To(HaveOccurred())
					var reasonableErr errutils.ReasonableError
					Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
					Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonMissingNamespace))
					Expect(reasonableErr.Error()).To(ContainSubstring("does not exist"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-ns-fail", replicaKind)
					nsCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(nsCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(nsCondition.Reason).To(Equal(repv1alpha1.ReasonMissingNamespace))
				})

				It("should skip without error when the target namespace does not exist and the policy is 'Skip'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-02")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-ns-skip", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-ns-skip", replicaKind)
					nsCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(nsCondition.Status).To(Equal(metav1.ConditionTrue))
					Expect(nsCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSkipped))
				})

				It("should create the namespace and sync the target when the namespace does not exist and the policy is 'Create' (the default)", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-02")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-ns-create", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					createdNs := &corev1.Namespace{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Name: "target-ns"}, createdNs)).To(Succeed())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-ns-create", replicaKind)
					nsCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(nsCondition.Status).To(Equal(metav1.ConditionTrue))
					Expect(nsCondition.Reason).To(Equal(repv1alpha1.ConditionReasonTargetSynced))

					createdSecret := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, createdSecret)).To(Succeed())
				})

				It("should fail when the target namespace is in deletion and the policy is 'Create' (the default)", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-02")

					targetNs := &corev1.Namespace{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Name: "target-ns-deletion"}, targetNs)).To(Succeed())
					controllerutil.AddFinalizer(targetNs, "test/block-deletion")
					Expect(env.Client(platformCluster).Update(env.Ctx, targetNs)).To(Succeed())
					Expect(env.Client(platformCluster).Delete(env.Ctx, targetNs)).To(Succeed())

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-ns-deletion", replicaKind)

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).To(HaveOccurred())
					var reasonableErr errutils.ReasonableError
					Expect(errors.As(err, &reasonableErr)).To(BeTrue(), "expected error to implement ReasonableError")
					Expect(reasonableErr.Reason()).To(Equal(repv1alpha1.ReasonNamespaceInDeletion))
					Expect(reasonableErr.Error()).To(ContainSubstring("is being deleted"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-ns-deletion", replicaKind)
					nsCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition to be set")
					Expect(nsCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(nsCondition.Reason).To(Equal(repv1alpha1.ReasonNamespaceInDeletion))
				})

			})

			Context("Deletion", func() {

				It("should delete managed resources and remove the finalizer when the Replica is deleted", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-03")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-delete", replicaKind)

					// Set management labels on the target secret to match the current replica kind and name.
					targetSecret := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, targetSecret)).To(Succeed())
					targetSecret.Labels = map[string]string{
						openmcpconst.ManagedByLabel:        providerName,
						repv1alpha1.ReplicaSourceKindLabel: strings.ToLower(replicaKind),
						repv1alpha1.ReplicaSourceNameLabel: rep.GetName(),
					}
					if replicaKind == repv1alpha1.KindReplica {
						targetSecret.Labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rep.GetNamespace()
					}
					Expect(env.Client(platformCluster).Update(env.Ctx, targetSecret)).To(Succeed())

					// Block deletion of the target secret so we can observe the intermediate state.
					controllerutil.AddFinalizer(targetSecret, "test/block-deletion")
					Expect(env.Client(platformCluster).Update(env.Ctx, targetSecret)).To(Succeed())

					Expect(env.Client(platformCluster).Delete(env.Ctx, rep)).To(Succeed())

					// First reconcile: target secret deletion requested, requeue requested
					res, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())
					Expect(res.RequeueAfter).To(BeNumerically(">", 0))
					Expect(res.RequeueAfter).To(BeNumerically("<=", time.Minute))

					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, targetSecret)).To(Succeed())
					Expect(targetSecret.DeletionTimestamp.IsZero()).To(BeFalse())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-delete", replicaKind)
					Expect(rep.GetStatus().Replicas).ToNot(BeEmpty())
					targetCondition, found := findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeTrue(), "expected a Target_* condition indicating the target is being deleted")
					Expect(targetCondition.Status).To(Equal(metav1.ConditionFalse))
					Expect(targetCondition.Reason).To(Equal(repv1alpha1.ConditionReasonWaitingForResourceDeletion))

					// Allow the target secret to be fully deleted, then reconcile again.
					controllerutil.RemoveFinalizer(targetSecret, "test/block-deletion")
					Expect(env.Client(platformCluster).Update(env.Ctx, targetSecret)).To(Succeed())

					res, err = ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())
					Expect(res.RequeueAfter).To(BeNumerically("<=", time.Minute))

					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "source-secret"}, targetSecret)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-delete", replicaKind)
					Expect(rep.GetStatus().Replicas).To(BeEmpty())
					_, found = findConditionWithPrefix(rep.GetStatus().Conditions, repv1alpha1.ConditionTypeTargetPrefix)
					Expect(found).To(BeFalse(), "expected no Target_* condition to remain after managed resources are deleted")

					// Final reconcile: finalizer removed
					_, err = ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep, err = getReplicaEquivalentWithError(env, "test-ns", "test-replica-delete", replicaKind)
					if err != nil {
						Expect(err).To(MatchError(apierrors.IsNotFound, "IsNotFound"), "expected the Replica to be deleted after finalizer removal")
					} else {
						Expect(rep.GetFinalizers()).ToNot(ContainElement(repv1alpha1.ReplicaFinalizer))
					}
				})

				It("should strip management labels from managed resources and remove the finalizer when the Replica is deleted and the retention policy is 'Keep'", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-03")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-retain", replicaKind)

					// Set management labels and the Keep deletion policy on the target secret.
					retainedSecret := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "retained-secret"}, retainedSecret)).To(Succeed())
					retainedSecret.Labels = map[string]string{
						openmcpconst.ManagedByLabel:            providerName,
						repv1alpha1.ReplicaSourceKindLabel:     strings.ToLower(replicaKind),
						repv1alpha1.ReplicaSourceNameLabel:     rep.GetName(),
						repv1alpha1.ReplicaDeletionPolicyLabel: repv1alpha1.ReplicaDeletionPolicyKeep,
					}
					if replicaKind == repv1alpha1.KindReplica {
						retainedSecret.Labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rep.GetNamespace()
					}
					Expect(env.Client(platformCluster).Update(env.Ctx, retainedSecret)).To(Succeed())

					Expect(env.Client(platformCluster).Delete(env.Ctx, rep)).To(Succeed())

					// Single reconcile: management labels stripped, secret kept, status cleared, finalizer removed on next pass
					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "target-ns", Name: "retained-secret"}, retainedSecret)).To(Succeed())
					Expect(retainedSecret.Labels).ToNot(HaveKey(openmcpconst.ManagedByLabel))
					Expect(retainedSecret.Labels).ToNot(HaveKey(repv1alpha1.ReplicaSourceKindLabel))
					Expect(retainedSecret.Labels).ToNot(HaveKey(repv1alpha1.ReplicaSourceNameLabel))
					Expect(retainedSecret.Labels).ToNot(HaveKey(repv1alpha1.ReplicaSourceNamespaceLabel))
					Expect(retainedSecret.Labels).ToNot(HaveKey(repv1alpha1.ReplicaDeletionPolicyLabel))
					Expect(retainedSecret.DeletionTimestamp.IsZero()).To(BeTrue(), "retained secret should not be deleted")

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-retain", replicaKind)
					Expect(rep.GetStatus().Replicas).To(BeEmpty())

					// Second reconcile: finalizer removed
					_, err = ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep, err = getReplicaEquivalentWithError(env, "test-ns", "test-replica-retain", replicaKind)
					if err != nil {
						Expect(err).To(MatchError(apierrors.IsNotFound, "IsNotFound"), "expected the Replica to be deleted after finalizer removal")
					} else {
						Expect(rep.GetFinalizers()).ToNot(ContainElement(repv1alpha1.ReplicaFinalizer))
					}
				})

				It("should delete both the Onto and NextTo copies when the target Cluster is in deletion", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-04")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-cluster-delete", replicaKind)

					// Set management labels on both managed copies.
					ontoSecret := &corev1.Secret{}
					Expect(env.Client("openmcp-system/workload-cluster").Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "source-secret"}, ontoSecret)).To(Succeed())
					ontoSecret.Labels = map[string]string{
						openmcpconst.ManagedByLabel:        providerName,
						repv1alpha1.ReplicaSourceKindLabel: strings.ToLower(replicaKind),
						repv1alpha1.ReplicaSourceNameLabel: rep.GetName(),
					}
					if replicaKind == repv1alpha1.KindReplica {
						ontoSecret.Labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rep.GetNamespace()
					}
					Expect(env.Client("openmcp-system/workload-cluster").Update(env.Ctx, ontoSecret)).To(Succeed())

					nextToSecret := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "source-secret"}, nextToSecret)).To(Succeed())
					nextToSecret.Labels = map[string]string{
						openmcpconst.ManagedByLabel:        providerName,
						repv1alpha1.ReplicaSourceKindLabel: strings.ToLower(replicaKind),
						repv1alpha1.ReplicaSourceNameLabel: rep.GetName(),
					}
					if replicaKind == repv1alpha1.KindReplica {
						nextToSecret.Labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rep.GetNamespace()
					}
					Expect(env.Client(platformCluster).Update(env.Ctx, nextToSecret)).To(Succeed())

					// Delete the Cluster resource (deletion timestamp set; the test finalizer blocks actual removal).
					workloadCluster := &clustersv1alpha1.Cluster{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "workload-cluster"}, workloadCluster)).To(Succeed())
					Expect(env.Client(platformCluster).Delete(env.Ctx, workloadCluster)).To(Succeed())

					// First reconcile: both copies deleted from their respective clusters, but references still in replica status
					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					Expect(env.Client("openmcp-system/workload-cluster").Get(env.Ctx, client.ObjectKey{Namespace: "test-ns", Name: "source-secret"}, ontoSecret)).
						To(MatchError(apierrors.IsNotFound, "IsNotFound"))
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "source-secret"}, nextToSecret)).
						To(MatchError(apierrors.IsNotFound, "IsNotFound"))

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-cluster-delete", replicaKind)
					Expect(rep.GetStatus().Replicas).To(ContainElements(
						repv1alpha1.CreatedResourcesWithType{
							Type: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
							Resources: []repv1alpha1.CreatedResource{
								{
									ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{Name: "source-secret", Namespace: "test-ns"},
									Cluster:                              &commonapi.ObjectReference{Name: "workload-cluster", Namespace: "openmcp-system"},
								},
								{
									ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{Name: "source-secret", Namespace: "openmcp-system"},
								},
							},
						},
					))

					// Second reconcile: resources already gone, status references cleared
					_, err = ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-cluster-delete", replicaKind)
					Expect(rep.GetStatus().Replicas).To(BeEmpty())
				})

				It("should not delete the shared NextTo copy when only one of two clusters in the same namespace is deleted", func() {
					env, _, ctrl := defaultTestSetup(withClusterReplicas, "testdata", "test-05")

					rep := getReplicaEquivalent(env, "test-ns", "test-replica-nextto-shared", replicaKind)

					// Set management labels on the shared NextTo copy.
					nextToSecret := &corev1.Secret{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "source-secret"}, nextToSecret)).To(Succeed())
					nextToSecret.Labels = map[string]string{
						openmcpconst.ManagedByLabel:        providerName,
						repv1alpha1.ReplicaSourceKindLabel: strings.ToLower(replicaKind),
						repv1alpha1.ReplicaSourceNameLabel: rep.GetName(),
					}
					if replicaKind == repv1alpha1.KindReplica {
						nextToSecret.Labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rep.GetNamespace()
					}
					Expect(env.Client(platformCluster).Update(env.Ctx, nextToSecret)).To(Succeed())

					// Delete cluster-a (deletion timestamp set; the test finalizer blocks actual removal).
					clusterA := &clustersv1alpha1.Cluster{}
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "workload-cluster-a"}, clusterA)).To(Succeed())
					Expect(env.Client(platformCluster).Delete(env.Ctx, clusterA)).To(Succeed())

					_, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
					Expect(err).ToNot(HaveOccurred())

					// The NextTo copy must still exist because cluster-b still requires it.
					Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKey{Namespace: "openmcp-system", Name: "source-secret"}, nextToSecret)).To(Succeed())
					Expect(nextToSecret.DeletionTimestamp.IsZero()).To(BeTrue(), "shared NextTo copy should not be deleted while still required by another cluster")

					rep = getReplicaEquivalent(env, "test-ns", "test-replica-nextto-shared", replicaKind)
					Expect(rep.GetStatus().Replicas).To(ContainElement(repv1alpha1.CreatedResourcesWithType{
						Type: metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
						Resources: []repv1alpha1.CreatedResource{
							{
								ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{Name: "source-secret", Namespace: "openmcp-system"},
							},
						},
					}))
				})

			})

		})
	}

})
