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
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	pluginBarmanCloudV1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	"github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/objectstore"
)

const (
	s3Name             = "s3"
	s3ClientName       = "s3-client"
	objectStoreName    = "source"
	srcClusterName     = "source"
	srcBackupName      = "source"
	restoreClusterName = "restore"
	size               = "1Gi"
	bucket             = "backups"
	// A separate secret also covers the Role granting access to it.
	keySecretName = "sse-c"
	keySecretKey  = "key"
	// Base64-encoded 256-bit AES key.
	sseCustomerKey = "3sz5Nv45cLh6FFQkP/+5wye8yJewBW9tuhVk4ZCaQbE="
)

// The e2e object store serves plain HTTP.
func newObjectStoreResources(namespace string) *objectstore.Resources {
	resources := objectstore.NewS3ObjectStoreResources(namespace, s3Name)
	container := &resources.Deployment.Spec.Template.Spec.Containers[0]
	container.Env = append(container.Env, corev1.EnvVar{Name: "RUSTFS_SSE_C_REQUIRE_TLS", Value: "false"})
	return resources
}

func newKeySecret(namespace string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      keySecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			keySecretKey: []byte(sseCustomerKey),
		},
	}
}

func newObjectStore(namespace string) *pluginBarmanCloudV1.ObjectStore {
	store := objectstore.NewS3ObjectStore(namespace, objectStoreName, s3Name)
	store.Spec.Configuration.AWS.SSECustomerKey = &api.SecretKeySelector{
		LocalObjectReference: api.LocalObjectReference{
			Name: keySecretName,
		},
		Key: keySecretKey,
	}
	return store
}

func newSrcCluster(namespace string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      srcClusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       1,
			ImagePullPolicy: corev1.PullAlways,
			Plugins: []cloudnativepgv1.PluginConfiguration{
				{
					Name: "barman-cloud.cloudnative-pg.io",
					Parameters: map[string]string{
						"barmanObjectName": objectStoreName,
					},
					IsWALArchiver: ptr.To(true),
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}
}

func newSrcBackup(namespace string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      srcBackupName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: srcClusterName,
			},
			Method: "plugin",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "barman-cloud.cloudnative-pg.io",
			},
		},
	}
}

func newRestoreCluster(namespace string) *cloudnativepgv1.Cluster {
	cluster := newSrcCluster(namespace)
	cluster.Name = restoreClusterName
	cluster.Spec.Bootstrap = &cloudnativepgv1.BootstrapConfiguration{
		Recovery: &cloudnativepgv1.BootstrapRecovery{
			Source: srcClusterName,
		},
	}
	cluster.Spec.ExternalClusters = []cloudnativepgv1.ExternalCluster{
		{
			Name: srcClusterName,
			PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
				Name: "barman-cloud.cloudnative-pg.io",
				Parameters: map[string]string{
					"barmanObjectName": objectStoreName,
					"serverName":       srcClusterName,
				},
			},
		},
	}
	return cluster
}
