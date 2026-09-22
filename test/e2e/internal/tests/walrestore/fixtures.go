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

package walrestore

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	barmanapi "github.com/cloudnative-pg/barman-cloud/pkg/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	pluginBarmanCloudV1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	"github.com/cloudnative-pg/plugin-barman-cloud/test/e2e/internal/objectstore"
)

const (
	s3Name          = "s3"
	objectStoreName = "source"
	clusterName     = "source"
	s3ClientName    = "s3-client"
	storageSize     = "1Gi"
	// walMaxParallel is the prefetch parallelism under test: for a regular WAL
	// request the plugin restores the requested segment and prefetches the next
	// ones, up to this many segments in total.
	walMaxParallel = 3
)

// newObjectStoreResources returns the S3 object store Deployment/Service/Secret/PVC.
func newObjectStoreResources(namespace string) *objectstore.Resources {
	return objectstore.NewS3ObjectStoreResources(namespace, s3Name)
}

// newObjectStore returns an S3-backed ObjectStore configured with the WAL
// prefetch parallelism (maxParallel) under test. Archiving with gzip makes the
// archived segments carry the ".gz" suffix that forged segments are copied from.
func newObjectStore(namespace string) *pluginBarmanCloudV1.ObjectStore {
	store := objectstore.NewS3ObjectStore(namespace, objectStoreName, s3Name)
	store.Spec.Configuration.Wal = &barmanapi.WalBackupConfiguration{
		MaxParallel: walMaxParallel,
		Compression: barmanapi.CompressionTypeGzip,
	}
	return store
}

// newCluster returns a 2-instance cluster that uses the plugin as its WAL
// archiver, so the standby drives WAL restore (and its prefetch/spool/
// end-of-wal-stream state machine) through the plugin.
func newCluster(namespace string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
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
			PostgresConfiguration: cloudnativepgv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_min_messages": "DEBUG4",
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: storageSize,
			},
		},
	}
}
