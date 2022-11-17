#!/bin/bash

source /etc/environments/hive.env
/propgen -label HIVE -render metastoresite -file /opt/hive-metastore/conf/metastore-site.xml

exec "/opt/hive-metastore/bin/start-metastore"