package resources

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
	"github.com/kubernetesbigdataeg/hive-operator/pkg/util"
)

func NewService(cr *bigdatav1alpha1.Hive, scheme *runtime.Scheme) *corev1.Service {

	labels := util.LabelsForHive(cr)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-svc",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    getPorts(cr),
		},
	}

	controllerutil.SetControllerReference(cr, service, scheme)

	return service

}

func getPorts(cr *bigdatav1alpha1.Hive) []corev1.ServicePort {
	return []corev1.ServicePort{
		{
			Name: "thrift",
			Port: cr.Spec.Service.ThriftPort,
		},
		{
			Name: "hive-server",
			Port: cr.Spec.Service.HiveServerPort,
		},
	}
}

func ReconcileService(ctx context.Context, client runtimeClient.Client, desired *corev1.Service) error {

	log := log.FromContext(ctx)
	log.Info("Reconciling Service")
	// Get the current Service
	current := &corev1.Service{}
	key := runtimeClient.ObjectKeyFromObject(desired)
	err := client.Get(ctx, key, current)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The Service does not exist yet, so we'll create it
			log.Info("Creating Service")
			err = client.Create(ctx, desired)
			if err != nil {
				log.Error(err, "unable to create Service")
				return fmt.Errorf("unable to create Service: %v", err)
			} else {
				return nil
			}
		}

		log.Error(err, "error getting Service")
		return fmt.Errorf("error getting Service: %v", err)
	}

	// Check if the current Service matches the desired one
	if !reflect.DeepEqual(current.Spec, desired.Spec) || !reflect.DeepEqual(current.ObjectMeta.Labels, desired.ObjectMeta.Labels) {
		// If it doesn't match, we'll update the current Service to match the desired one
		current.Spec = desired.Spec
		current.ObjectMeta.Labels = desired.ObjectMeta.Labels
		log.Info("Updating Service")
		err = client.Update(ctx, current)
		if err != nil {
			log.Error(err, "unable to update Service")
			return fmt.Errorf("unable to update Service: %v", err)
		}
	}

	// If we reach here, it means the Service is in the desired state
	return nil
}
