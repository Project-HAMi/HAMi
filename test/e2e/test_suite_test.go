package e2e

import (
	"flag"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/Project-HAMi/HAMi/test/utils"
)

func init() {
	testing.Init()
}

func TestInit(t *testing.T) {
	flag.Parse()
	utils.DefaultKubeConfigPath()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Test HAMi Suite")
}
