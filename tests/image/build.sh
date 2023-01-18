podman build . --tag docker.io/kubernetesbigdataeg/hive:3.1.3-1
podman login docker.io -u kubernetesbigdataeg
podman push docker.io/kubernetesbigdataeg/hive:3.1.3-1