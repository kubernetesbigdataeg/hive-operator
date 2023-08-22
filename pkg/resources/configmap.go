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

func NewConfigMap(cr *bigdatav1alpha1.Hive, scheme *runtime.Scheme) *corev1.ConfigMap {

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hive-config",
			Namespace: cr.Namespace,
			Labels:    util.LabelsForHive(cr),
		},
		Data: getData(cr),
	}

	controllerutil.SetControllerReference(cr, configMap, scheme)

	return configMap
}

func getData(cr *bigdatav1alpha1.Hive) map[string]string {

	data := make(map[string]string)

	data["hive.env"] = `
	export HIVE__hivesite__javax_jdo_option_ConnectionURL=` + cr.Spec.DbConnection.Url + `
	export HIVE__hivesite__javax_jdo_option_ConnectionDriverName="org.postgresql.Driver"
	export HIVE__hivesite__javax_jdo_option_ConnectionUserName=` + cr.Spec.DbConnection.UserName + `
	export HIVE__hivesite__javax_jdo_option_ConnectionPassword=` + cr.Spec.DbConnection.PassWord + `
	export HIVE__hivesite__metastore_expression_proxy="org.apache.hadoop.hive.metastore.DefaultPartitionExpressionProxy"
	export HIVE__hivesite__metastore_task_threads_always="org.apache.hadoop.hive.metastore.events.EventCleanerTask,org.apache.hadoop.hive.metastore.MaterializationsCacheCleanerTask"
	export HIVE__hivesite__datanucleus_autoCreateSchema="false"
	export HIVE__hivesite__hive_metastore_uris=` + cr.Spec.HiveMetastoreUris + `
	export HIVE__hivesite__hive_metastore_warehouse_dir="/var/lib/hive/warehouse"
	export HIVE__hivesite__hive_metastore_transactional_event_listeners="org.apache.hive.hcatalog.listener.DbNotificationListener,org.apache.kudu.hive.metastore.KuduMetastorePlugin"
	export HIVE__hivesite__hive_metastore_disallow_incompatible_col_type_changes="false"
	export HIVE__hivesite__hive_metastore_dml_events="true"
	export HIVE__hivesite__hive_metastore_event_db_notification_api_auth="false"
	`

	return data
}

/*
func getDriver(cr *bigdatav1alpha1.Hive) string {
	switch cr.DbConnection.DbType {
	case "mysql":
		return "com.mysql.jdbc.Driver"
	case "oracle":
		return "oracle.jdbc.driver.OracleDriver"
	case "mssql":
		return "com.microsoft.sqlserver.jdbc.SQLServerDriver"
	case "derby":
		return "org.apache.derby.jdbc.EmbeddedDriver"
	default:
		return "org.postgresql.Driver"
	}
}*/

func ReconcileConfigMap(ctx context.Context, client runtimeClient.Client, desired *corev1.ConfigMap) error {

	log := log.FromContext(ctx)
	log.Info("Reconciling ConfigMap")
	// Get the current ConfigMap
	current := &corev1.ConfigMap{}
	key := runtimeClient.ObjectKeyFromObject(desired)
	err := client.Get(ctx, key, current)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The ConfigMap does not exist yet, so we'll create it
			log.Info("Creating ConfigMap")
			err = client.Create(ctx, desired)
			if err != nil {
				log.Error(err, "unable to create ConfigMap")
				return fmt.Errorf("unable to create ConfigMap: %v", err)
			} else {
				return nil
			}
		}

		log.Error(err, "error getting ConfigMap")
		return fmt.Errorf("error getting ConfigMap: %v", err)
	}

	// Check if the current ConfigMap matches the desired one
	if !reflect.DeepEqual(current.Data, desired.Data) || !reflect.DeepEqual(current.BinaryData, desired.BinaryData) || !reflect.DeepEqual(current.ObjectMeta.Labels, desired.ObjectMeta.Labels) {
		// If it doesn't match, we'll update the current ConfigMap to match the desired one
		current.Data = desired.Data
		current.BinaryData = desired.BinaryData
		current.ObjectMeta.Labels = desired.ObjectMeta.Labels
		log.Info("Updating ConfigMap")
		err = client.Update(ctx, current)
		if err != nil {
			log.Error(err, "unable to update ConfigMap")
			return fmt.Errorf("unable to update ConfigMap: %v", err)
		}
	}

	// If we reach here, it means the ConfigMap is in the desired state
	return nil
}
