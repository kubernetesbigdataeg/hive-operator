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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
	"github.com/kubernetesbigdataeg/hive-operator/pkg/resources"
)

const hiveFinalizer = "bigdata.kubernetesbigdataeg.org/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableHive represents the status of the Deployment reconciliation
	typeAvailableHive = "Available"
	// typeDegradedHive represents the status used when the custom resource is deleted and the finalizer operations are must to occur.
	typeDegradedHive = "Degraded"
)

// HiveReconciler reconciles a Hive object
type HiveReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps;services,verbs=get;list;create;watch;update
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *HiveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	//
	// 1. Control-loop: checking if Hive CR exists
	//
	// Fetch the Hive instance
	// The purpose is check if the Custom Resource for the Kind Hive
	// is applied on the cluster if not we return nil to stop the reconciliation
	hive := &bigdatav1alpha1.Hive{}
	err := r.Get(ctx, req.NamespacedName, hive)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then, it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("hive resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get hive")
		return ctrl.Result{}, err
	}

	//
	// 2. Control-loop: Status to Unknown
	//
	// Let's just set the status as Unknown when no status are available
	if hive.Status.Conditions == nil || len(hive.Status.Conditions) == 0 {
		meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeAvailableHive, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, hive); err != nil {
			log.Error(err, "Failed to update Hive status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the hive Custom Resource after update the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raise the issue "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, hive); err != nil {
			log.Error(err, "Failed to re-fetch hive")
			return ctrl.Result{}, err
		}
	}

	//
	// 3. Control-loop: Let's add a finalizer
	//
	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(hive, hiveFinalizer) {
		log.Info("Adding Finalizer for Hive")
		if ok := controllerutil.AddFinalizer(hive, hiveFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, hive); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	//
	// 4. Control-loop: Instance marked for deletion
	//
	// Check if the Hive instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isHiveMarkedToBeDeleted := hive.GetDeletionTimestamp() != nil
	if isHiveMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(hive, hiveFinalizer) {
			log.Info("Performing Finalizer Operations for Hive before delete CR")

			// Let's add here an status "Downgrade" to define that this resource begin its process to be terminated.
			meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeDegradedHive,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", hive.Name)})

			if err := r.Status().Update(ctx, hive); err != nil {
				log.Error(err, "Failed to update Hive status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			r.doFinalizerOperationsForHive(hive)

			// TODO(user): If you add operations to the doFinalizerOperationsForHive method
			// then you need to ensure that all worked fine before deleting and updating the Downgrade status
			// otherwise, you should requeue here.

			// Re-fetch the hive Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, hive); err != nil {
				log.Error(err, "Failed to re-fetch hive")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeDegradedHive,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", hive.Name)})

			if err := r.Status().Update(ctx, hive); err != nil {
				log.Error(err, "Failed to update Hive status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for Hive after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(hive, hiveFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for Hive")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, hive); err != nil {
				log.Error(err, "Failed to remove finalizer for Hive")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	//
	// 5. Control-loop: Let's deploy/ensure our managed resources for Hive
	// - Job,
	// - ConfigMap,
	// - Service ClusterIP,
	// - Deployment
	//

	job := resources.NewJob(hive, r.Scheme)
	if err := resources.ReconcileJob(ctx, r.Client, job); err != nil {
		return ctrl.Result{}, err
	}

	deployment := resources.NewDeployment(hive, r.Scheme)
	if err := resources.ReconcileDeployment(ctx, r.Client, deployment); err != nil {
		return ctrl.Result{}, err
	}

	cm := resources.NewConfigMap(hive, r.Scheme)
	if err := resources.ReconcileConfigMap(ctx, r.Client, cm); err != nil {
		return ctrl.Result{}, err
	}

	svc := resources.NewService(hive, r.Scheme)
	if err := resources.ReconcileService(ctx, r.Client, svc); err != nil {
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeAvailableHive,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", hive.Name, hive.Spec.Size)})

	if err := r.Status().Update(ctx, hive); err != nil {
		log.Error(err, "Failed to update Hive status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeHive will perform the required operations before delete the CR.
func (r *HiveReconciler) doFinalizerOperationsForHive(cr *bigdatav1alpha1.Hive) {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of delete resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as depended of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// SetupWithManager sets up the controller with the Manager.
func (r *HiveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bigdatav1alpha1.Hive{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
