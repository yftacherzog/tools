package push

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = BeforeEach(func() {
	base := GinkgoT().TempDir()
	GinkgoT().Setenv("HELM_CONFIG_HOME", base)
	GinkgoT().Setenv("HELM_CACHE_HOME", filepath.Join(base, "cache"))
})

func TestPush(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "push")
}
