// nolint:goconst,prealloc
package replica_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	"github.com/openmcp-project/multicluster-provider/pkg/provider"
	"github.com/openmcp-project/multicluster-provider/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/api/install"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/replica"
)

const (
	providerName    = "replica"
	platformCluster = "platform"
)

// exampleTestSetup takes a parent directory and an example directory prefix. It expects the parent directory to contain a <prefix>_before and <prefix>_after directory.
// It loads all *.yaml files from the <parent>/<prefix>_before directory into a fake platform client, and returns a ReplicaController.
// All *.yaml files from the <parent>/<prefix>_after directory are returned as a mapping from cluster name to list of client.Object for comparison in the test.
// This is determined by an 'assign.json' file in the <parent>/<prefix>_after directory, which maps cluster names to lists of yaml files in the same directory. Every unassigned file is automatically mapped to the platform cluster. The assign.json file is optional; if it does not exist, all files are assigned to the platform cluster.
// The envSetupDir parameter, if non-empty, is expected to point to a directory containing yaml files. All yaml files will be put into the created fake cluster.
// Furthermore, if a yaml file is prefixed with "cluster_" and there is a subdirectory with the same name in the envSetupDir, then that Cluster resource will get a fake client containing all manifests from the subdirectory.
func exampleTestSetup(exampleDirPrefix, exampleName, envSetupDir string) (*testutils.ComplexEnvironment, multicluster.Provider, *replica.ReplicaController, map[multicluster.ClusterName][]client.Object) {
	GinkgoHelper()
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())

	envb := testutils.NewComplexEnvironmentBuilder().WithFakeClient(platformCluster, scheme)

	clusterPrefix := "cluster_"

	beforeDir := filepath.Join(exampleDirPrefix, exampleName+"_before")
	afterDir := filepath.Join(exampleDirPrefix, exampleName+"_after")

	beforeObjects, err := testutils.LoadObjects(beforeDir, scheme)
	if err != nil {
		Expect(err).ToNot(HaveOccurred())
	}

	// Load assign.json from afterDir to determine which cluster each file belongs to.
	// Files not listed in assign.json default to the platform cluster.
	fileAssignments := map[string][]multicluster.ClusterName{} // filename -> cluster names
	assignFile := filepath.Join(afterDir, "assign.json")
	if data, err := os.ReadFile(assignFile); err == nil {
		var raw map[string][]string
		Expect(json.Unmarshal(data, &raw)).To(Succeed())
		for clusterID, files := range raw {
			for _, f := range files {
				fileAssignments[f] = append(fileAssignments[f], multicluster.ClusterName(clusterID))
			}
		}
	}

	afterObjects := map[multicluster.ClusterName][]client.Object{}
	afterDirEntries, err := os.ReadDir(afterDir)
	Expect(err).ToNot(HaveOccurred())
	for _, entry := range afterDirEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		objects, err := testutils.LoadObjectsFromFile(filepath.Join(afterDir, entry.Name()), scheme)
		Expect(err).ToNot(HaveOccurred())
		clusters, assigned := fileAssignments[entry.Name()]
		if !assigned {
			clusters = []multicluster.ClusterName{multicluster.ClusterName(platformCluster)}
		}
		for _, cName := range clusters {
			afterObjects[cName] = append(afterObjects[cName], objects...)
		}
	}

	initObjects := map[multicluster.ClusterName][]client.Object{}

	if envSetupDir != "" {
		files, err := os.ReadDir(envSetupDir)
		Expect(err).ToNot(HaveOccurred())

		clusterMappings := map[string]multicluster.ClusterName{}
		clusterDirs := []os.DirEntry{}

		for _, file := range files {
			if !file.IsDir() && filepath.Ext(file.Name()) == ".yaml" {
				objects, err := testutils.LoadObjectsFromFile(filepath.Join(envSetupDir, file.Name()), scheme)
				Expect(err).ToNot(HaveOccurred())
				if cFileName, ok := strings.CutPrefix(file.Name(), clusterPrefix); ok {
					if len(objects) != 1 {
						Fail("expected exactly one Cluster object in file " + filepath.Join(envSetupDir, file.Name()))
					}
					cName := provider.ClusterName(objects[0].GetNamespace(), objects[0].GetName())
					clusterMappings[strings.TrimSuffix(cFileName, ".yaml")] = cName
					envb.WithFakeClient(string(cName), scheme)
				}

				initObjects[multicluster.ClusterName(platformCluster)] = append(initObjects[multicluster.ClusterName(platformCluster)], objects...)
			} else if file.IsDir() && strings.HasPrefix(file.Name(), clusterPrefix) {
				// we can only load the cluster objects after we know the cluster name, so we store the directory for later processing
				clusterDirs = append(clusterDirs, file)
			}
		}

		for _, clusterDir := range clusterDirs {
			objects, err := testutils.LoadObjects(filepath.Join(envSetupDir, clusterDir.Name()), scheme)
			Expect(err).ToNot(HaveOccurred())
			if cName, ok := clusterMappings[strings.TrimPrefix(clusterDir.Name(), clusterPrefix)]; !ok {
				Fail("found cluster directory " + clusterDir.Name() + " but no corresponding cluster yaml file")
			} else {
				initObjects[cName] = append(initObjects[cName], objects...)
			}
		}
	}

	initObjects[multicluster.ClusterName(platformCluster)] = append(initObjects[multicluster.ClusterName(platformCluster)], beforeObjects...)

	for cName, objs := range initObjects {
		envb.WithInitObjects(string(cName), objs...)
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

	return env, prov, ctrl, afterObjects
}

var _ = Describe("ReplicaController", Serial, func() {

	Context("Example Tests", func() {

		exampleDirPath := filepath.Join("..", "..", "docs", "examples")
		exampleDirContents, err := os.ReadDir(exampleDirPath)
		Expect(err).ToNot(HaveOccurred())

		exampleNames := []string{}
		for _, exampleGroup := range exampleDirContents {
			if exampleGroup.IsDir() {
				if exampleGroup.Name() == "environment" {
					continue
				}
				exampleTestCases, err := os.ReadDir(filepath.Join(exampleDirPath, exampleGroup.Name()))
				Expect(err).ToNot(HaveOccurred())
				for _, testCase := range exampleTestCases {
					if testCase.IsDir() {
						if name, ok := strings.CutSuffix(testCase.Name(), "_before"); ok {
							exampleNames = append(exampleNames, filepath.Join(exampleGroup.Name(), name))
						}
					}
				}
			}
		}

		describeTableArguments := []any{
			func(exampleDirPrefix, exampleName, envSetupDir string) {
				env, _, ctrl, validationObjects := exampleTestSetup(exampleDirPrefix, exampleName, envSetupDir)

				// Reconcile all Replica and ClusterReplica resources in the platform cluster
				cReps := &repv1alpha1.ClusterReplicaList{}
				Expect(env.Client(platformCluster).List(env.Ctx, cReps)).To(Succeed())
				reps := &repv1alpha1.ReplicaList{}
				Expect(env.Client(platformCluster).List(env.Ctx, reps)).To(Succeed())
				replicas := make([]repv1alpha1.ReplicaEquivalent, 0, len(cReps.Items)+len(reps.Items))
				for _, cr := range cReps.Items {
					replicas = append(replicas, &cr)
				}
				for _, r := range reps.Items {
					replicas = append(replicas, &r)
				}

				for _, rep := range replicas {
					success := false
					for range 5 {
						rr, err := ctrl.Reconcile(env.Ctx, mcreconcile.Request{Request: testutils.RequestFromObject(rep)})
						Expect(err).ToNot(HaveOccurred())
						if rr.RequeueAfter == 0 {
							success = true
							break
						}
					}
					Expect(success).To(BeTrue(), "Reconcile did not complete successfully for Replica %s/%s", rep.GetNamespace(), rep.GetName())
				}

				// validate existence of expected objects in each cluster
				for cName, expectedObjects := range validationObjects {
					cl := env.Client(string(cName))
					Expect(cl).ToNot(BeNil(), "Client for cluster %s should not be nil", cName)
					for _, expected := range expectedObjects {
						actual := expected.DeepCopyObject().(client.Object)
						Expect(cl.Get(env.Ctx, client.ObjectKeyFromObject(expected), actual)).To(Succeed(), "Error fetching expected object %s/%s from cluster %s", expected.GetNamespace(), expected.GetName(), cName)
						Expect(resourcesDiff(expected, actual)).To(BeEmpty(), "Expected object %s/%s in cluster %s does not match actual", expected.GetNamespace(), expected.GetName(), cName)
					}
				}
			},
		}
		for _, exampleName := range exampleNames {
			describeTableArguments = append(describeTableArguments, Entry(exampleName, filepath.Join(exampleDirPath, filepath.Dir(exampleName)), filepath.Base(exampleName), filepath.Join(exampleDirPath, "environment")))
		}

		DescribeTable("Example Test Cases", describeTableArguments...)
	})

})

// resourcesDiff compares two client.Object resources, ignoring ResourceVersion and ManagedFields.
// Returns a human-readable diff string, or empty string if the objects are equal.
func resourcesDiff(obj1, obj2 client.Object) string {
	u1, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj1)
	if err != nil {
		return err.Error()
	}
	u2, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj2)
	if err != nil {
		return err.Error()
	}
	for _, u := range []map[string]any{u1, u2} {
		if meta, ok := u["metadata"].(map[string]any); ok {
			delete(meta, "resourceVersion")
			delete(meta, "managedFields")
		}
		if status, ok := u["status"].(map[string]any); ok {
			if conditions, ok := status["conditions"].([]any); ok {
				for _, c := range conditions {
					if cond, ok := c.(map[string]any); ok {
						delete(cond, "lastTransitionTime")
					}
				}
			}
		}
	}
	return cmp.Diff(u1, u2)
}
