package resources

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
	"github.com/kubernetesbigdataeg/hive-operator/pkg/util"
)

func NewDeployment(cr *bigdatav1alpha1.Hive, scheme *runtime.Scheme) *appsv1.Deployment {

	labels := util.LabelsForHive(cr)

	replicas := cr.Spec.Size

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
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
					Containers: getContainers(cr),
					Volumes:    getVolumes(),
				},
			},
		},
	}

	controllerutil.SetControllerReference(cr, deployment, scheme)

	return deployment
}

func getContainers(cr *bigdatav1alpha1.Hive) []corev1.Container {

	container := []corev1.Container{
		{
			Name:            "hive",
			Image:           util.GetImage(cr),
			ImagePullPolicy: corev1.PullAlways,
			Command:         []string{"/opt/hive/bin/entrypoint.sh"},
			Env:             getEnvVars(cr),
			Resources:       getResources(cr),
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{
							IntVal: int32(9083),
						},
					},
				},
				InitialDelaySeconds: 30,
				PeriodSeconds:       60,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{
							IntVal: int32(9083),
						},
					},
				},
				InitialDelaySeconds: 30,
				PeriodSeconds:       60,
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
	}

	return container
}

func getEnvVars(cr *bigdatav1alpha1.Hive) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "HIVE_DB_EXTERNAL",
			//Value: cr.Spec.Deployment.EnvVar.HIVE_DB_EXTERNAL,
			Value: "true",
		},
		{
			Name:  "HIVE_DB_NAME",
			Value: cr.Spec.Deployment.EnvVar.HIVE_DB_NAME,
		},
	}
}

func getVolumes() []corev1.Volume {
	return []corev1.Volume{
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
	}

}

func getResources(cr *bigdatav1alpha1.Hive) corev1.ResourceRequirements {

	if len(cr.Spec.Deployment.Resources.Requests) > 0 || len(cr.Spec.Deployment.Resources.Limits) > 0 {
		return cr.Spec.Deployment.Resources
	} else {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(util.CpuRequest),
				corev1.ResourceMemory: resource.MustParse(util.MemoryRequest),
			},
		}
	}

}

func ReconcileDeployment(ctx context.Context, client runtimeClient.Client, desired *appsv1.Deployment) error {

	log := log.FromContext(ctx)
	log.Info("Reconciling Deployment")
	// Get the current Deployment
	current := &appsv1.Deployment{}
	key := runtimeClient.ObjectKeyFromObject(desired)
	err := client.Get(ctx, key, current)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The Deployment does not exist yet, so we'll create it
			log.Info("Creating Deployment")
			err = client.Create(ctx, desired)
			if err != nil {
				log.Error(err, "unable to create Deployment")
				return fmt.Errorf("unable to create Deployment: %v", err)
			} else {
				return nil
			}
		} else {
			return fmt.Errorf("error getting Deployment: %v", err)
		}
	}

	// TODO: Check if it has changed in more specific fields.
	// Check if the current Deployment matches the desired one
	if !reflect.DeepEqual(current.Spec, desired.Spec) {
		// If it doesn't match, we'll update the current Deployment to match the desired one
		current.Spec.Replicas = desired.Spec.Replicas
		current.Spec.Template = desired.Spec.Template
		log.Info("Updating Deployment")
		err = client.Update(ctx, current)
		if err != nil {
			log.Error(err, "unable to update Deployment")
			return fmt.Errorf("unable to update Deployment: %v", err)
		}
	}

	// If we reach here, it means the Deployment is in the desired state
	return nil
}
