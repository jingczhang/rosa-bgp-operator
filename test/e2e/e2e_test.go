/*
Copyright 2026.

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

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CUDN BGP Routing Operator", func() {
	Context("E2E-01: Operator pod starts", func() {
		It("should have a Running controller-manager pod", func(ctx context.Context) {
			Eventually(func(g Gomega) {
				pods, err := clientset.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: "control-plane=controller-manager",
				})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pods.Items).NotTo(BeEmpty(), "no controller-manager pods found")

				var running bool
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning {
						running = true
						break
					}
				}
				g.Expect(running).To(BeTrue(), "no controller-manager pod in Running phase")
			}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
		})
	})
})
