podman build . --tag docker.io/kubernetesbigdataeg/hive-metastore:3.1.2-1
podman login docker.io
podman push docker.io/kubernetesbigdataeg/hive-metastore:3.1.2-1