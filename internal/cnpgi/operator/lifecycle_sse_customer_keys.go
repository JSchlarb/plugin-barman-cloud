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
	"context"
	"path"

	corev1 "k8s.io/api/core/v1"

	barmancloudv1 "github.com/cloudnative-pg/plugin-barman-cloud/api/v1"
	"github.com/cloudnative-pg/plugin-barman-cloud/internal/cnpgi/operator/config"
)

const sseCustomerKeysVolumeName = "barman-sse-customer-keys"

func (impl LifecycleImplementation) collectSSECustomerKeys(
	ctx context.Context,
	pluginConfiguration *config.PluginConfiguration,
) ([]corev1.VolumeProjection, error) {
	var result []corev1.VolumeProjection
	projected := make(map[string]bool)

	for _, barmanObjectKey := range pluginConfiguration.GetReferredBarmanObjectsKey() {
		var objectStore barmancloudv1.ObjectStore
		if err := impl.Client.Get(ctx, barmanObjectKey, &objectStore); err != nil {
			return nil, err
		}

		s3Credentials := objectStore.Spec.Configuration.AWS
		if s3Credentials == nil || s3Credentials.SSECustomerKey == nil {
			continue
		}

		keyPath := path.Join(s3Credentials.SSECustomerKey.Name, s3Credentials.SSECustomerKey.Key)
		if projected[keyPath] {
			continue
		}
		projected[keyPath] = true

		result = append(result, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: s3Credentials.SSECustomerKey.Name},
				Items:                []corev1.KeyToPath{{Key: s3Credentials.SSECustomerKey.Key, Path: keyPath}},
			},
		})
	}

	return result, nil
}
