/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package leaderelection

import (
	"sync"
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

	ginkgo.Context("When object is other type", func() {
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
	duration := int32(15)
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holderIdentity,
			AcquireTime:          &now,
			LeaseDurationSeconds: &duration,
		},
	}
}

func assertNotifyElected(lm LeaderManager, leaderNotify <-chan struct{}) {
	ginkgo.GinkgoHelper()
	pt := &struct{}{}
	g.Eventually(leaderNotify).Should(g.Receive(pt))
	g.Expect(lm.IsLeader()).Should(g.BeTrue())
}

func assertElectedWithoutNotifying(lm LeaderManager, leaderNotify <-chan struct{}) {
	ginkgo.GinkgoHelper()
	g.Consistently(leaderNotify).ShouldNot(g.Receive())
	g.Expect(lm.IsLeader()).Should(g.BeTrue())
}

func assertSynced(synced bool) {
	ginkgo.GinkgoHelper()
	g.Expect(synced).Should(g.BeTrue())
}

func assertNotElected(lm LeaderManager, leaderNotify <-chan struct{}, synced bool) {
	ginkgo.GinkgoHelper()
	g.Consistently(leaderNotify).ShouldNot(g.Receive())
	g.Expect(lm.IsLeader()).ShouldNot(g.BeTrue())
	g.Expect(synced).Should(g.BeFalse())
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
	var leaderNotify chan struct{}
	var lock sync.RWMutex
	var synced bool

	var doSync = func() {
		lock.Lock()
		defer lock.Unlock()
		synced = true
	}

	// Testing constructor
	ginkgo.Describe("Initializing LeaderManager", func() {
		ginkgo.It("should create a new LeaderManager", func() {
			lm := NewLeaderManager("dev", "kube-system", "hami-scheduler", LeaderCallbacks{})
			g.Expect(lm).ShouldNot(g.BeNil())
		})
	})

	ginkgo.BeforeEach(func() {
		initLeaseHostname = "dev"
		hostname = "dev"
		namespace = "kube-system"
		name = "hami-scheduler"
		leaderNotify = make(chan struct{}, 1)
		synced = false
	})

	// Delay creating after all BeforeEach nodes so that each case can override the params with nested BeforeEach node
	ginkgo.JustBeforeEach(func() {
		initLease = generateLease(initLeaseHostname, namespace, name)
		callbacks := LeaderCallbacks{
			OnStartedLeading: func() {
				leaderNotify <- struct{}{}
			},
			OnStoppedLeading: func() {
				synced = false
			},
		}
		lm = NewLeaderManager(hostname, namespace, name, callbacks)
	})

	ginkgo.Describe("When events of unrelated lease triggered", func() {
		ginkgo.It("should ignore lease with another name", func() {
			lease := generateLease(hostname, namespace, "anotherLease")
			lm.OnAdd(lease, true)
			assertNotElected(lm, leaderNotify, synced)
		})
		ginkgo.It("should ignore lease with another namespace", func() {
			lease := generateLease(hostname, "anotherNamespace", name)
			lm.OnAdd(lease, true)
			assertNotElected(lm, leaderNotify, synced)
		})
	})

	ginkgo.Describe("When current instance is leader from the beginning", func() {
		ginkgo.It("should be notified as leader", func() {
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm, leaderNotify)
		})
	})

	ginkgo.Describe("When current instance is elected after sometime", func() {
		ginkgo.BeforeEach(func() {
			initLeaseHostname = "another"
		})

		ginkgo.It("should be notified as leader", func() {
			ginkgo.By("leader is another one at first")
			lm.OnAdd(initLease, true)
			assertNotElected(lm, leaderNotify, synced)

			ginkgo.By("elected as leader")
			newLease := acquireLeaseWithNewHost(initLease, hostname)
			lm.OnUpdate(initLease, newLease)
			assertNotifyElected(lm, leaderNotify)
		})
	})

	ginkgo.Describe("When lease renewed without changing holder", func() {
		ginkgo.It("should not be notified as leader again", func() {
			ginkgo.By("elected as leader")
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm, leaderNotify)

			ginkgo.By("renewing lease")
			newLease := renewLeaseAtNow(initLease)
			lm.OnUpdate(initLease, newLease)

			assertElectedWithoutNotifying(lm, leaderNotify)
		})
	})

	ginkgo.Describe("When get challenged and is not leader anymore", func() {
		ginkgo.It("should not be leader anymore", func() {
			ginkgo.By("Elected as leader")
			lm.OnAdd(initLease, true)
			assertNotifyElected(lm, leaderNotify)

			ginkgo.By("Setting synced to true")
			doSync()
			assertSynced(synced)

			ginkgo.By("Challenged and not leader anymore")
			challengerLease := acquireLeaseWithNewHost(initLease, "another")
			lm.OnUpdate(initLease, challengerLease)
			assertNotElected(lm, leaderNotify, synced)
		})
	})

	ginkgo.Describe("When lease is deleted", func() {
		ginkgo.Describe("we are leader", func() {
			ginkgo.It("should not be leader anymore unless elected", func() {
				ginkgo.By("Elected as leader")
				lm.OnAdd(initLease, true)
				assertNotifyElected(lm, leaderNotify)

				ginkgo.By("Setting synced to true")
				doSync()
				assertSynced(synced)

				ginkgo.By("Lease is deleted")
				lm.OnDelete(initLease)
				assertNotElected(lm, leaderNotify, synced)

				newLease := acquireLeaseWithNewHost(initLease, hostname)
				ginkgo.By("Elected as leader again")
				lm.OnAdd(newLease, false)
				assertNotifyElected(lm, leaderNotify)
			})
		})

		ginkgo.Describe("we are not leader", func() {
			ginkgo.BeforeEach(func() {
				initLeaseHostname = "another"
			})
			ginkgo.It("should still not be leader", func() {
				ginkgo.By("Not leader at first")
				lm.OnAdd(initLease, true)
				assertNotElected(lm, leaderNotify, synced)

				ginkgo.By("Lease is deleted")
				lm.OnDelete(initLease)
				assertNotElected(lm, leaderNotify, synced)

				ginkgo.By("Elected as leader")
				newLease := acquireLeaseWithNewHost(initLease, hostname)
				lm.OnAdd(newLease, false)
				assertNotifyElected(lm, leaderNotify)
			})
		})

	})
})

var _ = ginkgo.Describe("Nil checks for callbacks", func() {
	var initLease *coordinationv1.Lease
	var hostname, namespace, name string
	var lm *leaderManager

	ginkgo.BeforeEach(func() {
		hostname = "dev"
		namespace = "kube-system"
		name = "hami-scheduler"
		initLease = generateLease(hostname, namespace, name)
	})

	ginkgo.Context("When callbacks are nil", func() {
		ginkgo.BeforeEach(func() {
			// Create LeaderManager with empty callbacks (nil functions)
			lm = NewLeaderManager(hostname, namespace, name, LeaderCallbacks{})
		})

		ginkgo.It("should not panic when OnStartedLeading is called in onAdd", func() {
			g.Expect(func() {
				lm.OnAdd(initLease, true)
			}).ShouldNot(g.Panic())
			g.Expect(lm.IsLeader()).Should(g.BeTrue())
		})

		ginkgo.It("should not panic when OnStartedLeading is called in onUpdate", func() {
			anotherLease := acquireLeaseWithNewHost(initLease, "another")
			lm.OnAdd(anotherLease, true)

			g.Expect(func() {
				newLease := acquireLeaseWithNewHost(initLease, hostname)
				lm.OnUpdate(anotherLease, newLease)
			}).ShouldNot(g.Panic())
			g.Expect(lm.IsLeader()).Should(g.BeTrue())
		})

		ginkgo.It("should not panic when OnStoppedLeading is called in onUpdate", func() {
			lm.OnAdd(initLease, true)

			g.Expect(func() {
				challengerLease := acquireLeaseWithNewHost(initLease, "another")
				lm.OnUpdate(initLease, challengerLease)
			}).ShouldNot(g.Panic())
			g.Expect(lm.IsLeader()).Should(g.BeFalse())
		})

		ginkgo.It("should not panic when OnStoppedLeading is called in onDelete", func() {
			lm.OnAdd(initLease, true)

			g.Expect(func() {
				lm.OnDelete(initLease)
			}).ShouldNot(g.Panic())
			g.Expect(lm.IsLeader()).Should(g.BeFalse())
		})
	})

	ginkgo.Context("When only OnStartedLeading is provided", func() {
		var notified bool

		ginkgo.BeforeEach(func() {
			notified = false
			callbacks := LeaderCallbacks{
				OnStartedLeading: func() {
					notified = true
				},
				// OnStoppedLeading is nil
			}
			lm = NewLeaderManager(hostname, namespace, name, callbacks)
		})

		ginkgo.It("should call OnStartedLeading but not panic on OnStoppedLeading", func() {
			lm.OnAdd(initLease, true)
			g.Expect(notified).Should(g.BeTrue())

			g.Expect(func() {
				lm.OnDelete(initLease)
			}).ShouldNot(g.Panic())
		})
	})

	ginkgo.Context("When only OnStoppedLeading is provided", func() {
		var stopped bool

		ginkgo.BeforeEach(func() {
			stopped = false
			callbacks := LeaderCallbacks{
				// OnStartedLeading is nil
				OnStoppedLeading: func() {
					stopped = true
				},
			}
			lm = NewLeaderManager(hostname, namespace, name, callbacks)
		})

		ginkgo.It("should not panic on OnStartedLeading but call OnStoppedLeading", func() {
			g.Expect(func() {
				lm.OnAdd(initLease, true)
			}).ShouldNot(g.Panic())

			lm.OnDelete(initLease)
			g.Expect(stopped).Should(g.BeTrue())
		})
	})
})

var _ = ginkgo.Describe("Nil checks for lease fields", func() {
	var hostname, namespace, name string
	var lm *leaderManager

	ginkgo.BeforeEach(func() {
		hostname = "dev"
		namespace = "kube-system"
		name = "hami-scheduler"
		lm = NewLeaderManager(hostname, namespace, name, LeaderCallbacks{})
	})

	ginkgo.Context("When lease is nil", func() {
		ginkgo.It("isHolderOf should return false", func() {
			result := lm.isHolderOf(nil)
			g.Expect(result).Should(g.BeFalse())
		})
	})

	ginkgo.Context("When lease.Spec.HolderIdentity is nil", func() {
		ginkgo.It("isHolderOf should return false", func() {
			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: nil, // nil holder identity
				},
			}
			result := lm.isHolderOf(lease)
			g.Expect(result).Should(g.BeFalse())
		})
	})

	ginkgo.Context("When observedLease is nil", func() {
		ginkgo.It("isLeaseValid should return false", func() {
			lm.observedLease = nil
			result := lm.isLeaseValid(time.Now())
			g.Expect(result).Should(g.BeFalse())
		})

		ginkgo.It("IsLeader should return false", func() {
			lm.observedLease = nil
			result := lm.IsLeader()
			g.Expect(result).Should(g.BeFalse())
		})
	})

	ginkgo.Context("When LeaseDurationSeconds is nil", func() {
		ginkgo.It("isLeaseValid should return false", func() {
			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity:       func() *string { s := hostname; return &s }(),
					LeaseDurationSeconds: nil, // nil duration
				},
			}
			lm.setObservedRecord(lease)
			result := lm.isLeaseValid(time.Now())
			g.Expect(result).Should(g.BeFalse())
		})
	})
})

var _ = ginkgo.Describe("DummyLeaderManager", func() {
	var lm LeaderManager
	var elected bool
	var leaderNotify chan struct{}

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
		leaderNotify = make(chan struct{}, 1)
		close(leaderNotify)
	})
	ginkgo.JustBeforeEach(func() {
		lm = NewDummyLeaderManager(elected)
		g.Expect(lm).ShouldNot(g.BeNil())
	})

	ginkgo.It("should always be leader but not notified", func() {
		assertElectedWithoutNotifying(lm, leaderNotify)
	})
})
