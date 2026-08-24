package namespace_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNamespaceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Controller Test Suite")
}
