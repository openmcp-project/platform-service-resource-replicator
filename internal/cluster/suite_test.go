package cluster_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Handler Test Suite")
}
