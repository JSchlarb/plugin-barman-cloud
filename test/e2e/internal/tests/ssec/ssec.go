/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package ssec

import (
	"fmt"
	"strings"
	"time"

	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internalClient "github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/client"
	internalCluster "github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/cluster"
	"github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/command"
	"github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/deployment"
	nmsp "github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/namespace"
	"github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/objectstore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	sidecarContainerName = "plugin-barman-cloud"
	keysVolumeName       = "barman-sse-customer-keys"
)

var _ = Describe("SSE-C", func() {
	var namespace *corev1.Namespace
	var cl client.Client

	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "sse-c")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("should backup and restore a cluster with a customer-provided key", func(ctx SpecContext) {
		ns := namespace.Name
		clientSet, cfg, err := internalClient.NewClientSet()
		Expect(err).NotTo(HaveOccurred())
		exec := func(pod, container string, args ...string) (string, error) {
			stdout, _, err := command.ExecInPod(ctx, clientSet, cfg, ns, pod, container, args...)
			return stdout, err
		}
		psql := func(pod, query string) (string, error) {
			return exec(pod, "postgres", "psql", "-tAc", query)
		}

		By("starting the ObjectStore deployment")
		Expect(newObjectStoreResources(ns).Create(ctx, cl)).To(Succeed())

		By("creating the SSE-C key secret and the ObjectStore")
		Expect(cl.Create(ctx, newKeySecret(ns))).To(Succeed())
		Expect(cl.Create(ctx, newObjectStore(ns))).To(Succeed())

		By("deploying the S3 client used to inspect the objects")
		Expect(cl.Create(ctx, objectstore.NewS3ClientDeployment(ns, s3ClientName, s3Name))).To(Succeed())

		By("creating the Cluster")
		src := newSrcCluster(ns)
		Expect(cl.Create(ctx, src)).To(Succeed())

		By("having the Cluster ready")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: src.Name, Namespace: ns}, src)).To(Succeed())
			g.Expect(internalCluster.IsReady(*src)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		srcPod := fmt.Sprintf("%v-1", src.Name)

		By("verifying the key is mounted in the sidecar only")
		var pod corev1.Pod
		Expect(cl.Get(ctx, types.NamespacedName{Name: srcPod, Namespace: ns}, &pod)).To(Succeed())
		Expect(pod.Spec.InitContainers).To(ContainElement(SatisfyAll(
			HaveField("Name", sidecarContainerName),
			HaveField("VolumeMounts", ContainElement(HaveField("Name", keysVolumeName))),
		)))
		Expect(pod.Spec.Containers).To(ContainElement(SatisfyAll(
			HaveField("Name", "postgres"),
			HaveField("VolumeMounts", Not(ContainElement(HaveField("Name", keysVolumeName)))),
		)))

		By("adding data to PostgreSQL")
		_, err = psql(srcPod, "CREATE TABLE test (i int); INSERT INTO test VALUES (1);")
		Expect(err).NotTo(HaveOccurred())

		By("creating a backup")
		backup := newSrcBackup(ns)
		Expect(cl.Create(ctx, backup)).To(Succeed())

		By("waiting for the backup to complete")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: ns}, backup)).To(Succeed())
			g.Expect(backup.Status.Phase).To(BeEquivalentTo(cloudnativepgv1.BackupPhaseCompleted))
		}).Within(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("waiting for the S3 client to become ready")
		Eventually(func(g Gomega) {
			ready, err := deployment.IsReady(ctx, cl, types.NamespacedName{Name: s3ClientName, Namespace: ns})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ready).To(BeTrue())
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		var s3ClientPods corev1.PodList
		Expect(cl.List(ctx, &s3ClientPods,
			client.InNamespace(ns),
			client.MatchingLabels{"app": s3ClientName})).To(Succeed())
		Expect(s3ClientPods.Items).NotTo(BeEmpty())
		s3Client := s3ClientPods.Items[0].Name

		By("verifying the backup data is readable only with the key")
		dataKey := fmt.Sprintf("%s/base/%s/data.tar", srcClusterName, backup.Status.BackupID)
		dataURI := fmt.Sprintf("s3://%s/%s", bucket, dataKey)
		out, err := exec(s3Client, s3ClientName, "aws", "s3", "ls", dataURI)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
		_, err = exec(s3Client, s3ClientName, "sh", "-c", fmt.Sprintf(
			"echo %s | base64 -d > /tmp/key && "+
				"aws s3 cp --sse-c AES256 --sse-c-key fileb:///tmp/key %s /tmp/with-key",
			sseCustomerKey, dataURI))
		Expect(err).NotTo(HaveOccurred())
		// ExecInPod drops the output on a non-zero exit.
		out, err = exec(s3Client, s3ClientName, "sh", "-c", fmt.Sprintf(
			"aws s3api get-object --bucket %s --key %s /tmp/without-key 2>&1 || true",
			bucket, dataKey))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(SatisfyAll(
			ContainSubstring("InvalidRequest"),
			ContainSubstring("Server Side Encryption"),
		))

		By("adding data after the backup")
		_, err = psql(srcPod, "SELECT pg_switch_wal(); INSERT INTO test VALUES (2)")
		Expect(err).NotTo(HaveOccurred())
		out, err = psql(srcPod, "SELECT pg_walfile_name(pg_switch_wal())")
		Expect(err).NotTo(HaveOccurred())
		lastWAL := strings.TrimSpace(out)
		Expect(lastWAL).To(HaveLen(24))

		By("waiting for the WAL holding the new data to be archived")
		walURI := fmt.Sprintf("s3://%s/%s/wals/%s/%s", bucket, srcClusterName, lastWAL[:16], lastWAL)
		Eventually(func(g Gomega) {
			out, err := exec(s3Client, s3ClientName, "aws", "s3", "ls", walURI)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty())
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("restoring the backup")
		dst := newRestoreCluster(ns)
		Expect(cl.Create(ctx, dst)).To(Succeed())

		By("having the restored Cluster ready")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: dst.Name, Namespace: ns}, dst)).To(Succeed())
			g.Expect(internalCluster.IsReady(*dst)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		By("verifying the data exists in the restored instance")
		out, err = psql(fmt.Sprintf("%v-1", dst.Name), "SELECT count(*) FROM test;")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(BeEquivalentTo("2\n"))
	})
})
