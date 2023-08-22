/*
Copyright 2023.

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

package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
)

var _ = Describe("Hive controller", func() {
	Context("Hive controller test", func() {

		const HiveName = "test-hive"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      HiveName,
				Namespace: HiveName,
			},
		}

		typeNamespaceName := types.NamespacedName{Name: HiveName, Namespace: HiveName}

		BeforeEach(func() {
			By("Creating the Namespace to perform the tests")
			err := k8sClient.Create(ctx, namespace)
			Expect(err).To(Not(HaveOccurred()))

			By("Setting the Image ENV VAR which stores the Operand image")
			err = os.Setenv("HIVE_IMAGE", "example.com/image:test")
			Expect(err).To(Not(HaveOccurred()))
		})

		AfterEach(func() {
			// TODO(user): Attention if you improve this code by adding other context test you MUST
			// be aware of the current delete namespace limitations. More info: https://book.kubebuilder.io/reference/envtest.html#testing-considerations
			By("Deleting the Namespace to perform the tests")
			_ = k8sClient.Delete(ctx, namespace)

			By("Removing the Image ENV VAR which stores the Operand image")
			_ = os.Unsetenv("HIVE_IMAGE")
		})

		It("should successfully reconcile a custom resource for Hive", func() {
			By("Creating the custom resource for the Kind Hive")
			hive := &bigdatav1alpha1.Hive{}
			err := k8sClient.Get(ctx, typeNamespaceName, hive)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				hive := &bigdatav1alpha1.Hive{
					ObjectMeta: metav1.ObjectMeta{
						Name:      HiveName,
						Namespace: namespace.Name,
					},
					Spec: bigdatav1alpha1.HiveSpec{
						Size:              1,
						BaseImageVersion:  "3.1.3-1",
						HiveMetastoreUris: "thrift://hive-svc.default.svc.cluster.local:9083",
						DbConnection: bigdatav1alpha1.DbConnection{
							UserName: "postgres",
							PassWord: "postgres",
							Url:      "jdbc:postgresql://postgres-svc.default.svc.cluster.local:5432/metastore",
						},
						Deployment: bigdatav1alpha1.DeploymentSpec{
							EnvVar: bigdatav1alpha1.EnvVar{
								HIVE_DB_NAME: "metastore",
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
						Service: bigdatav1alpha1.ServiceSpec{
							ThriftPort:     9083,
							HiveServerPort: 10000,
						},
					},
				}

				err = k8sClient.Create(ctx, hive)
				Expect(err).To(Not(HaveOccurred()))
			}

			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &bigdatav1alpha1.Hive{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			hiveReconciler := &HiveReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = hiveReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))

			By("Checking if Deployment was successfully created in the reconciliation")
			Eventually(func() error {
				found := &appsv1.Deployment{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, 3*time.Minute, time.Second).Should(Succeed())

			By("Checking the latest Status Condition added to the Hive instance")
			Eventually(func() error {
				if hive.Status.Conditions != nil && len(hive.Status.Conditions) != 0 {
					latestStatusCondition := hive.Status.Conditions[len(hive.Status.Conditions)-1]
					expectedLatestStatusCondition := metav1.Condition{Type: typeAvailableHive,
						Status: metav1.ConditionTrue, Reason: "Reconciling",
						Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", hive.Name, hive.Spec.Size)}
					if latestStatusCondition != expectedLatestStatusCondition {
						return fmt.Errorf("The latest status condition added to the hive instance is not as expected")
					}
				}
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
