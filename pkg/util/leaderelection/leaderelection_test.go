package leaderelection

import (
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	g "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/cache"
)

func TestInit(t *testing.T) {
	g.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Test leader election Suite")
}

var _ = ginkgo.Describe("objectToLease", func() {
	var object any

	ginkgo.Context("When object is lease", func() {
		ginkgo.BeforeEach(func() {
			object = &coordinationv1.Lease{}
		})
		ginkgo.It("should get lease", func() {
			result := objectToLease(object)
			g.Expect(result).ShouldNot(g.BeNil())
			g.Expect(result).Should(g.BeAssignableToTypeOf(&coordinationv1.Lease{}))
		})
	})

	ginkgo.Context("When object state is DeletedFinalStateUnknown", func() {
		ginkgo.BeforeEach(func() {
			object = cache.DeletedFinalStateUnknown{
				Obj: &coordinationv1.Lease{},
			}
		})
		ginkgo.It("should get lease", func() {
			result := objectToLease(object)
			g.Expect(result).ShouldNot(g.BeNil())
			g.Expect(result).Should(g.BeAssignableToTypeOf(&coordinationv1.Lease{}))
		})
	})

	ginkgo.Context("When obejct is other type", func() {
		ginkgo.BeforeEach(func() {
			object = "invalid object"
		})
		ginkgo.It("should return nil", func() {
			result := objectToLease(object)
			g.Expect(result).Should(g.BeNil())
		})
	})
})

func generateHolderIdentity(hostname string) string {
	return hostname + "_" + string(uuid.NewUUID())
}

func generateLease(hostname, namespace, name string) *coordinationv1.Lease {
	holderIdentity := generateHolderIdentity(hostname)
	now := metav1.NewMicroTime(time.Now())
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: &holderIdentity,
			AcquireTime:    &now,
		},
	}
}

func assertNotifyElected(lm LeaderManager) {
	pt := &struct{}{}
	g.Eventually(lm.LeaderNotifyChan()).Should(g.Receive(pt))
	g.Expect(lm.IsLeader()).Should(g.BeTrue())
}

func assertElectedWithoutNotifying(lm LeaderManager) {
	g.Consistently(lm.LeaderNotifyChan()).ShouldNot(g.Receive())
	g.Expect(lm.IsLeader()).Should(g.BeTrue())
}

func assertNotElected(lm LeaderManager) {
	g.Consistently(lm.LeaderNotifyChan()).ShouldNot(g.Receive())
	g.Expect(lm.IsLeader()).ShouldNot(g.BeTrue())
}

func renewLeaseAtNow(lease *coordinationv1.Lease) *coordinationv1.Lease {
	now := metav1.NewMicroTime(time.Now())
	new := lease.DeepCopy()
	new.Spec.RenewTime = &now
	return new
}

func acquireLeaseWithNewHost(lease *coordinationv1.Lease, hostname string) *coordinationv1.Lease {
	holderIdentity := generateHolderIdentity(hostname)
	new := lease.DeepCopy()
	new.Spec.HolderIdentity = &holderIdentity
	return new
}

var _ = ginkgo.Describe("LeaderManager", func() {
	var initLease *coordinationv1.Lease
	var initLeaseHostname, hostname, namespace, name string
	var lm LeaderManager

	// Testing constructor
	ginkgo.Describe("Initializing LeaderManager", func() {
		ginkgo.It("should create a new LeaderManager", func() {
			lm := NewLeaderManager("dev", "kube-system", "hami-scheduler")
			g.Expect(lm).ShouldNot(g.BeNil())
		})
	})

	ginkgo.BeforeEach(func() {
		initLeaseHostname = "dev"
		hostname = "dev"
		namespace = "kube-system"
		name = "hami-scheduler"
	})

	// Delay creating after all BeforeEach nodes so that each case can override the params with nested BeforeEach node
	ginkgo.JustBeforeEach(func() {
		initLease = generateLease(initLeaseHostname, namespace, name)
		lm = NewLeaderManager(hostname, namespace, name)
	})

	ginkgo.Describe("When events of unrelated lease triggered", func() {
		ginkgo.It("should ignore lease with another name", func() {
			lease := generateLease(hostname, namespace, "anotherLease")
			lm.OnAdd(lease, true)
			assertNotElected(lm)
		})
		ginkgo.It("should ignore lease with another namespace", func() {
			lease := generateLease(hostname, "anotherNamspace", name)
			lm.OnAdd(lease, true)
			assertNotElected(lm)
		})
	})

	ginkgo.Describe("When current instance is leader from the begging", func() {
		ginkgo.It("should be notified as leader", func() {
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm)
		})
	})

	ginkgo.Describe("When current instance is elected after sometime", func() {
		ginkgo.BeforeEach(func() {
			initLeaseHostname = "another"
		})

		ginkgo.It("should be notified as leader", func() {
			ginkgo.By("leader is another one at first")
			lm.OnAdd(initLease, true)
			assertNotElected(lm)

			ginkgo.By("elected as leader")
			newLease := acquireLeaseWithNewHost(initLease, hostname)
			lm.OnUpdate(initLease, newLease)
			assertNotifyElected(lm)
		})
	})

	ginkgo.Describe("When lease renewed without changing holder", func() {
		ginkgo.It("should not be notified as leader again", func() {
			ginkgo.By("elected as leader")
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm)

			ginkgo.By("renewing lease")
			newLease := renewLeaseAtNow(initLease)
			lm.OnUpdate(initLease, newLease)

			assertElectedWithoutNotifying(lm)
		})
	})

	ginkgo.Describe("When get challenged and is not leader anymore", func() {
		ginkgo.It("should not be leader anymore", func() {
			ginkgo.By("Elected as leader")
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm)

			ginkgo.By("Challenged and not leader anymore")
			challengerLease := acquireLeaseWithNewHost(initLease, "another")
			lm.OnUpdate(initLease, challengerLease)
			assertNotElected(lm)
		})
	})

	ginkgo.Describe("When lease is deleted", func() {
		ginkgo.Describe("we are leader", func() {
			ginkgo.It("shoud not be leader anymore unless elected", func() {
				ginkgo.By("Elected as leader")
				lm.OnAdd(initLease, true)
				assertNotifyElected(lm)

				ginkgo.By("Lease is deleted")
				lm.OnDelete(initLease)
				assertNotElected(lm)

				newLease := acquireLeaseWithNewHost(initLease, hostname)
				ginkgo.By("Elected as leader again")
				lm.OnAdd(newLease, false)
				assertNotifyElected(lm)
			})
		})

		ginkgo.Describe("we are not leader", func() {
			ginkgo.BeforeEach(func() {
				initLeaseHostname = "another"
			})
			ginkgo.It("should still not be leader", func() {
				ginkgo.By("Not leader at first")
				lm.OnAdd(initLease, true)
				assertNotElected(lm)

				ginkgo.By("Lease is deleted")
				lm.OnDelete(initLease)
				assertNotElected(lm)

				ginkgo.By("Elected as leader")
				newLease := acquireLeaseWithNewHost(initLease, hostname)
				lm.OnAdd(newLease, false)
				assertNotifyElected(lm)
			})
		})

	})
})

var _ = ginkgo.Describe("DummyLeaderManager", func() {
	var lm LeaderManager
	var elected bool

	// Testing constructor
	ginkgo.Describe("Initializing DummyLeaderManager", func() {
		ginkgo.It("should create a new DummyLeaderManager", func() {
			elected = true
			lm = NewDummyLeaderManager(elected)
			g.Expect(lm).ShouldNot(g.BeNil())
		})
	})

	ginkgo.BeforeEach(func() {
		elected = true
	})
	ginkgo.JustBeforeEach(func() {
		lm = NewDummyLeaderManager(elected)
		g.Expect(lm).ShouldNot(g.BeNil())
	})

	ginkgo.It("should always be leader but not notified", func() {
		assertElectedWithoutNotifying(lm)
	})
})
