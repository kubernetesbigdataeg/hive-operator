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
	"strings"
	"time"

	_ "github.com/lib/pq"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
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

// The following markers are used to generate the rules permissions (RBAC) on config/rbac using controller-gen
// when the command <make manifests> is executed.
// To know more about markers see: https://book.kubebuilder.io/reference/markers.html

//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bigdata.kubernetesbigdataeg.org,resources=hives/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=configmaps;services,verbs=get;list;create;watch
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// It is essential for the controller's reconciliation loop to be idempotent. By following the Operator
// pattern you will create Controllers which provide a reconcile function
// responsible for synchronizing resources until the desired state is reached on the cluster.
// Breaking this recommendation goes against the design principles of controller-runtime.
// and may lead to unforeseen consequences such as resources becoming stuck and requiring manual intervention.
// For further info:
// - About Operator Pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - About Controllers: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
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

	/*/ Check if the schema is already created
	schemaExists, err := r.isSchemaCreated()
	if err != nil {
		return ctrl.Result{}, err
	}

	if !schemaExists {*/

	// Crea o actualiza Job
	jobFound := &batchv1.Job{}
	if err := r.ensureResource(ctx, hive, r.jobForHiveInitSchema, jobFound, "hive-initschema", "Job"); err != nil {
		return ctrl.Result{}, err
	}
	/*// If the Job has not completed, requeue the reconciliation after 5 seconds
	if jobFound.Status.Succeeded == 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}*/

	// ConfigMap
	configMapFound := &corev1.ConfigMap{}
	if err := r.ensureResource(ctx, hive, r.defaultConfigMapForHive, configMapFound, "hive-config", "ConfigMap"); err != nil {
		return ctrl.Result{}, err
	}

	// Service
	serviceFound := &corev1.Service{}
	if err := r.ensureResource(ctx, hive, r.serviceForHive, serviceFound, "hive-svc", "Service"); err != nil {
		return ctrl.Result{}, err
	}

	// Deployment
	deploymentFound := &appsv1.Deployment{}
	if err := r.ensureResource(ctx, hive, r.deploymentForHive, deploymentFound, hive.Name, "Deployment"); err != nil {
		return ctrl.Result{}, err
	}

	//
	// 6. Control-loop: Check the number of replicas
	//
	// The CRD API is defining that the Hive type, have a HiveSpec.Size field
	// to set the quantity of Deployment instances is the desired state on the cluster.
	// Therefore, the following code will ensure the Deployment size is the same as defined
	// via the Size spec of the Custom Resource which we are reconciling.
	size := hive.Spec.Size
	if deploymentFound.Spec.Replicas == nil {
		log.Error(nil, "Spec is not initialized for Deployment", "Deployment.Namespace", deploymentFound.Namespace, "Deployment.Name", deploymentFound.Name)
		return ctrl.Result{}, fmt.Errorf("spec is not initialized for Deployment %s/%s", deploymentFound.Namespace, deploymentFound.Name)
	}
	if *deploymentFound.Spec.Replicas != size {
		deploymentFound.Spec.Replicas = &size
		if err = r.Update(ctx, deploymentFound); err != nil {
			log.Error(err, "Failed to update Deployment",
				"Deployment.Namespace", deploymentFound.Namespace, "Deployment.Name", deploymentFound.Name)

			// Re-fetch the hive Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, hive); err != nil {
				log.Error(err, "Failed to re-fetch hive")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeAvailableHive,
				Status: metav1.ConditionFalse, Reason: "Resizing",
				Message: fmt.Sprintf("Failed to update the size for the custom resource (%s): (%s)", hive.Name, err)})

			if err := r.Status().Update(ctx, hive); err != nil {
				log.Error(err, "Failed to update Hive status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeAvailableHive,
		Status: metav1.ConditionTrue, Reason: "Reconciling",
		Message: fmt.Sprintf("Deployment for custom resource (%s) with %d replicas created successfully", hive.Name, size)})

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

func (r *HiveReconciler) jobForHiveInitSchema(hive *bigdatav1alpha1.Hive, resourceName string) (client.Object, error) {
	image, err := imageForHive()
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: hive.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "hivemeta",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/opt/hive/bin/schematool"},
							Args: []string{
								"--verbose",
								"-initSchema",
								"-dbType",
								"postgres",
								"-userName",
								"postgres",
								"-passWord",
								"postgres",
								"-url",
								"jdbc:postgresql://postgres-svc.default.svc.cluster.local:5432/metastore",
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			BackoffLimit: new(int32),
		},
	}

	*job.Spec.BackoffLimit = 4

	return job, nil
}

func (r *HiveReconciler) defaultConfigMapForHive(Hive *bigdatav1alpha1.Hive, resourceName string) (client.Object, error) {
	namespace := Hive.Namespace

	configMapData := make(map[string]string, 0)
	hiveEnv := `
	export HIVE__hivesite__javax_jdo_option_ConnectionURL="jdbc:postgresql://postgres-svc.default.svc.cluster.local:5432/metastore"
    export HIVE__hivesite__javax_jdo_option_ConnectionDriverName="org.postgresql.Driver"
    export HIVE__hivesite__javax_jdo_option_ConnectionUserName="postgres"
    export HIVE__hivesite__javax_jdo_option_ConnectionPassword="postgres"
    export HIVE__hivesite__metastore_expression_proxy="org.apache.hadoop.hive.metastore.DefaultPartitionExpressionProxy"
    export HIVE__hivesite__metastore_task_threads_always="org.apache.hadoop.hive.metastore.events.EventCleanerTask,org.apache.hadoop.hive.metastore.MaterializationsCacheCleanerTask"
    export HIVE__hivesite__datanucleus_autoCreateSchema="false"
    export HIVE__hivesite__hive_metastore_uris="thrift://hive-svc.` + namespace + `.svc.cluster.local:9083"
    export HIVE__hivesite__hive_metastore_warehouse_dir="/var/lib/hive/warehouse"
    export HIVE__hivesite__hive_metastore_transactional_event_listeners="org.apache.hive.hcatalog.listener.DbNotificationListener,org.apache.kudu.hive.metastore.KuduMetastorePlugin"
    export HIVE__hivesite__hive_metastore_disallow_incompatible_col_type_changes="false"
    export HIVE__hivesite__hive_metastore_dml_events="true"
    export HIVE__hivesite__hive_metastore_event_db_notification_api_auth="false"
	`

	configMapData["hive.env"] = hiveEnv
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: Hive.Namespace,
		},
		Data: configMapData,
	}

	if err := ctrl.SetControllerReference(Hive, configMap, r.Scheme); err != nil {
		return nil, err
	}

	return configMap, nil
}

func (r *HiveReconciler) serviceForHive(Hive *bigdatav1alpha1.Hive, resourceName string) (client.Object, error) {

	labels := labelsForHive(Hive.Name)
	s := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: Hive.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name: "thrift",
					Port: 9083,
				},
				{
					Name: "hive-server",
					Port: 10000,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	if err := ctrl.SetControllerReference(Hive, s, r.Scheme); err != nil {
		return nil, err
	}

	return s, nil
}

// deploymentForHive returns a Hive Deployment object
func (r *HiveReconciler) deploymentForHive(hive *bigdatav1alpha1.Hive, resourceName string) (client.Object, error) {

	labels := labelsForHive(hive.Name)

	replicas := hive.Spec.Size

	image, err := imageForHive()
	if err != nil {
		return nil, err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: hive.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "hive",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/opt/hive/bin/entrypoint.sh"},
							Env: []corev1.EnvVar{
								{
									Name:  "HIVE_DB_EXTERNAL",
									Value: "true",
								},
								{
									Name:  "HIVE_DB_NAME",
									Value: "metastore",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "thrift",
									ContainerPort: 9083,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "hive-server",
									ContainerPort: 10000,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "hive-env",
									MountPath: "/etc/environments",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "hive-env",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "hive-config",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return deployment, nil
}

// labelsForHive returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForHive(name string) map[string]string {
	var imageTag string
	image, err := imageForHive()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{
		"app.kubernetes.io/name":       "Hive",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "hive-operator",
		"app.kubernetes.io/created-by": "controller-manager",
		"app":                          "hive",
	}
}

// imageForHive gets the Operand image which is managed by this controller
// from the HIVE_IMAGE environment variable defined in the config/manager/manager.yaml
func imageForHive() (string, error) {
	var imageEnvVar = "HIVE_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("unable to find %s environment variable with the image", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
// Note that the Deployment will be also watched in order to ensure its
// desirable state on the cluster
func (r *HiveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bigdatav1alpha1.Hive{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *HiveReconciler) ensureResource(ctx context.Context, hive *bigdatav1alpha1.Hive, createResourceFunc func(*bigdatav1alpha1.Hive, string) (client.Object, error), foundResource client.Object, resourceName string, resourceType string) error {
	log := log.FromContext(ctx)
	err := r.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: hive.Namespace}, foundResource)
	if err != nil && apierrors.IsNotFound(err) {
		resource, err := createResourceFunc(hive, resourceName)
		if err != nil {
			log.Error(err, fmt.Sprintf("Failed to define new %s resource for Hive", resourceType))

			// The following implementation will update the status
			meta.SetStatusCondition(&hive.Status.Conditions, metav1.Condition{Type: typeAvailableHive,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create %s for the custom resource (%s): (%s)", resourceType, hive.Name, err)})

			if err := r.Status().Update(ctx, hive); err != nil {
				log.Error(err, "Failed to update Hive status")
				return err
			}

			return err
		}

		log.Info(fmt.Sprintf("Creating a new %s", resourceType),
			fmt.Sprintf("%s.Namespace", resourceType), resource.GetNamespace(), fmt.Sprintf("%s.Name", resourceType), resource.GetName())

		if err = r.Create(ctx, resource); err != nil {
			log.Error(err, fmt.Sprintf("Failed to create new %s", resourceType),
				fmt.Sprintf("%s.Namespace", resourceType), resource.GetNamespace(), fmt.Sprintf("%s.Name", resourceType), resource.GetName())
			return err
		}

		time.Sleep(5 * time.Second)

		if err := r.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: hive.Namespace}, foundResource); err != nil {
			log.Error(err, fmt.Sprintf("Failed to get newly created %s", resourceType))
			return err
		}

	} else if err != nil {
		log.Error(err, fmt.Sprintf("Failed to get %s", resourceType))
		return err
	}

	return nil
}

/*
func (r *HiveReconciler) isSchemaCreated() (bool, error) {
	connStr := "postgres://postgres:postgres@postgres-svc.default.svc.cluster.local:5432/metastore?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return false, fmt.Errorf("failed to connect to the database: %v", err)
	}
	defer db.Close()

	query := `SELECT EXISTS (
		SELECT FROM information_schema.schemata
		WHERE schema_name = 'hive'
	);`
	var exists bool
	err = db.QueryRow(query).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if the schema exists: %v", err)
	}

	return exists, nil
}
*/
