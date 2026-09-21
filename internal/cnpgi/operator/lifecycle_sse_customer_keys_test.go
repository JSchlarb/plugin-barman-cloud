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

package operator

import (
	barmanapi "github.com/cloudnative-pg/barman-cloud/pkg/api"
	barmanUtils "github.com/cloudnative-pg/barman-cloud/pkg/utils"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	"github.com/cloudnative-pg/plugin-barman-cloud/internal/cnpgi/operator/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SSE-C customer keys", func() {
	keyA := &machineryapi.SecretKeySelector{
		LocalObjectReference: machineryapi.LocalObjectReference{Name: "sse-c-key-a"}, Key: "key",
	}
	keyB := &machineryapi.SecretKeySelector{
		LocalObjectReference: machineryapi.LocalObjectReference{Name: "sse-c-key-b"}, Key: "key",
	}

	store := func(name string, key *machineryapi.SecretKeySelector) runtime.Object {
		return &barmancloudv1.ObjectStore{
			TypeMeta:   metav1.TypeMeta{Kind: "ObjectStore", APIVersion: barmancloudv1.GroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: barmancloudv1.ObjectStoreSpec{Configuration: barmanapi.BarmanObjectStoreConfiguration{
				DestinationPath: "s3://bucket/" + name,
				BarmanCredentials: barmanapi.BarmanCredentials{AWS: &barmanapi.S3Credentials{
					InheritFromIAMRole: true, SSECustomerKey: key,
				}},
			}},
		}
	}

	collect := func(ctx SpecContext, archive, replicaSource string, stores ...runtime.Object) []string {
		s := runtime.NewScheme()
		barmancloudv1.AddKnownTypes(s)
		impl := LifecycleImplementation{Client: fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(stores...).Build()}
		projections, err := impl.collectSSECustomerKeys(ctx, &config.PluginConfiguration{
			Cluster:                       &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}},
			BarmanObjectName:              archive,
			ReplicaSourceBarmanObjectName: replicaSource,
		})
		Expect(err).ToNot(HaveOccurred())

		var result []string
		for _, p := range projections {
			for _, item := range p.Secret.Items {
				result = append(result, p.Secret.Name+"/"+item.Key+" -> "+item.Path)
			}
		}
		return result
	}

	It("projects the key of every referred object store to <secret>/<key>", func(ctx SpecContext) {
		Expect(collect(ctx, "store-a", "store-b", store("store-a", keyA), store("store-b", keyB))).To(ConsistOf(
			"sse-c-key-a/key -> sse-c-key-a/key",
			"sse-c-key-b/key -> sse-c-key-b/key",
		))
	})

	It("projects a key shared by several object stores once", func(ctx SpecContext) {
		Expect(collect(ctx, "store-a", "store-a2", store("store-a", keyA), store("store-a2", keyA))).To(HaveLen(1))
	})

	It("mounts the keys only in the sidecar, where barman-cloud reads them", func() {
		spec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}}
		Expect(reconcilePodSpec(&cnpgv1.Cluster{}, spec, "postgres", corev1.Container{Name: "plugin-barman-cloud"},
			sidecarConfiguration{sseCustomerKeys: []corev1.VolumeProjection{{Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: "sse-c-key-a"},
			}}}})).To(Succeed())

		mountPaths := func(c corev1.Container) []string {
			var result []string
			for _, m := range c.VolumeMounts {
				result = append(result, m.MountPath)
			}
			return result
		}
		Expect(mountPaths(spec.Containers[0])).ToNot(ContainElement(barmanUtils.SSECustomerKeyDirectory))
		Expect(spec.InitContainers).To(HaveLen(1))
		Expect(mountPaths(spec.InitContainers[0])).To(ContainElement(barmanUtils.SSECustomerKeyDirectory))
		Expect(barmanUtils.SSECustomerKeyFilePath(keyA)).To(HavePrefix(barmanUtils.SSECustomerKeyDirectory + "/"))
	})
})
