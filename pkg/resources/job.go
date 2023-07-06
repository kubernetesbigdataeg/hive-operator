package resources

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
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

func NewJob(cr *bigdatav1alpha1.Hive, scheme *runtime.Scheme) *batchv1.Job {

	labels := util.LabelsForHive(cr)

	backoffLimit := int32(4)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-initschema",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "check-schema",
							Image:           "postgres:latest",
							ImagePullPolicy: corev1.PullAlways,
							Command: []string{
								"/bin/sh",
								"-c",
								getCheckSchemaScript(cr),
							},
							VolumeMounts: getVolumeMount(),
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "hivemeta",
							Image:           util.GetImage(cr),
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"/bin/sh", "-c"},
							Args: []string{
								"if [ -f /tmp/must-create-schema ]; then /opt/hive/bin/schematool --verbose -initSchema -dbType postgres -userName " + cr.Spec.DbConnection.UserName + " -passWord " + cr.Spec.DbConnection.PassWord + " -url " + cr.Spec.DbConnection.Url + "; fi",
							},
							VolumeMounts: getVolumeMount(),
						},
					},
					Volumes:       getVolume(),
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}

	controllerutil.SetControllerReference(cr, job, scheme)

	return job
}

func getCheckSchemaScript(cr *bigdatav1alpha1.Hive) string {

	cleanURL := strings.TrimPrefix(cr.Spec.DbConnection.Url, "jdbc:postgresql://")

	splitURL := strings.Split(cleanURL, "/")
	dbName := splitURL[1]

	hostPortSplit := strings.Split(splitURL[0], ":")
	host := hostPortSplit[0]
	port := hostPortSplit[1]

	checkSchemaScript := `
	#!/bin/bash

	# Configuración de la conexión a PostgreSQL
	DB_HOST=` + host + `
	DB_PORT=` + port + `
	DB_NAME=` + dbName + `
	DB_USER=` + cr.Spec.DbConnection.UserName + `
	DB_PASSWORD=` + cr.Spec.DbConnection.PassWord + `

	SQL_COMMAND="SELECT table_name FROM information_schema.tables WHERE table_name = 'DBS';"

	TABLE_EXIST=$(PGPASSWORD=$DB_PASSWORD psql -t -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "$SQL_COMMAND")

	if [ -z "$TABLE_EXIST" ]; then
		touch /tmp/must-create-schema
	else
		echo "Hive schema has been initialized."
	fi

	`
	return checkSchemaScript

}

func getVolumeMount() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "tmp",
			MountPath: "/tmp",
		},
	}
}

func getVolume() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
}

func ReconcileJob(ctx context.Context, client runtimeClient.Client, desired *batchv1.Job) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Job")

	// Get the current Job
	current := &batchv1.Job{}
	key := runtimeClient.ObjectKeyFromObject(desired)
	err := client.Get(ctx, key, current)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "error getting Job")
		return fmt.Errorf("error getting Job: %v", err)
	}

	if current.Status.Succeeded > 0 {
		// Job has completed successfully, nothing to do.
		log.Info("Job has completed successfully")
		return nil
	}

	// Check if the Job is currently running
	if current.Status.Active > 0 {
		log.Info("Job is currently running")
		return nil
	}

	if apierrors.IsNotFound(err) {
		// The Job does not exist yet, so we'll create it
		log.Info("Creating Job")
		if err := client.Create(ctx, desired); err != nil {
			log.Error(err, "unable to create Job")
			return fmt.Errorf("unable to create Job: %v", err)
		}
		return nil
	}

	if !reflect.DeepEqual(current.Spec, desired.Spec) || !reflect.DeepEqual(current.ObjectMeta.Labels, desired.ObjectMeta.Labels) {
		log.Info("Job spec or labels don't match, updating Job")

		// If it doesn't match, we'll update the current Job to match the desired one
		current.Spec = desired.Spec
		current.ObjectMeta.Labels = desired.ObjectMeta.Labels

		err = client.Update(ctx, current)
		if err != nil {
			log.Error(err, "unable to update Job")
			return fmt.Errorf("unable to update Job: %v", err)
		}
	}

	// If we reach here, it means the Job is in the desired state
	return nil
}
