package replica_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	providerName      = "replica"
	platformCluster   = "platform"
	onboardingCluster = "onboarding"
)

func TestReplicaController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Replica Controller Test Suite")
}
