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

package objectstore

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// NewS3ClientDeployment returns an AWS CLI deployment for the object store
// created by NewS3ObjectStoreResources.
func NewS3ClientDeployment(namespace, name, s3Name string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: name,
							// renovate: datasource=docker depName=amazon/aws-cli versioning=docker
							// Version: 2.36.32
							Image:   "docker.io/amazon/aws-cli@sha256:f630107e3eadb6479fa441631bbf50d15cf354a6ace85b6028bf6b3e5c69c605",
							Command: []string{"sleep", "infinity"},
							Env: []corev1.EnvVar{
								{
									Name:  "AWS_ENDPOINT_URL",
									Value: "http://" + s3Name + ":9000",
								},
								{
									Name: "AWS_ACCESS_KEY_ID",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: s3Name},
											Key:                  "ACCESS_KEY_ID",
										},
									},
								},
								{
									Name: "AWS_SECRET_ACCESS_KEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: s3Name},
											Key:                  "ACCESS_SECRET_KEY",
										},
									},
								},
								{
									Name:  "AWS_DEFAULT_REGION",
									Value: "us-east-1",
								},
								// Not every S3-compatible store supports the default
								// CRC checksums.
								{
									Name:  "AWS_REQUEST_CHECKSUM_CALCULATION",
									Value: "when_required",
								},
								{
									Name:  "AWS_RESPONSE_CHECKSUM_VALIDATION",
									Value: "when_required",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
						},
					},
				},
			},
		},
	}
}
