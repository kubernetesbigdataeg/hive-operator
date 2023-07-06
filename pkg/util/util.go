package util

import (
	bigdatav1alpha1 "github.com/kubernetesbigdataeg/hive-operator/api/v1alpha1"
	_ "github.com/lib/pq"
)

const (
	baseImage        = "docker.io/kubernetesbigdataeg/hive"
	baseImageVersion = "3.1.3-1"
	MemoryRequest    = "2g"
	CpuRequest       = "2"
)

// labelsForHive returns the labels for selecting the resources
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func LabelsForHive(cr *bigdatav1alpha1.Hive) map[string]string {

	return map[string]string{
		"app.kubernetes.io/name":       "Hive",
		"app.kubernetes.io/instance":   cr.Name,
		"app.kubernetes.io/version":    GetImageVersion(cr),
		"app.kubernetes.io/part-of":    "hive-operator",
		"app.kubernetes.io/created-by": "controller-manager",
		"app":                          "hive",
	}

}

func GetImage(cr *bigdatav1alpha1.Hive) string {
	return baseImage + ":" + GetImageVersion(cr)
}

func GetImageVersion(cr *bigdatav1alpha1.Hive) string {
	if len(cr.Spec.BaseImageVersion) > 0 {
		return cr.Spec.BaseImageVersion
	}
	return baseImageVersion
}
