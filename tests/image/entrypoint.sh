#!/bin/bash

source /etc/environments/hive.env
/propgen -label HIVE -render hivesite -file /opt/hive/conf/hive-site.xml

exec /opt/hive/bin/hive --service metastore --hiveconf hive.root.logger=DEBUG,console