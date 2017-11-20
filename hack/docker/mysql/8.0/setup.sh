#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

GOPATH=$(go env GOPATH)
REPO_ROOT=$GOPATH/src/github.com/k8sdb/mysql

source "$REPO_ROOT/hack/libbuild/common/kubedb_image.sh"

IMG=mysql
TAG=8.0

docker_names=( \
	#"db" \   #library/mysql image is used
	"util" \
)

build() {
    pushd $REPO_ROOT/hack/docker/mysql/8.0
    for name in "${docker_names[@]}"
    do
        cd $name
        docker build -t kubedb/$IMG:$TAG-$name .
        cd ..
    done
	popd
}

docker_push() {
    for name in "${docker_names[@]}"
    do
        docker push kubedb/$IMG:$TAG-$name
    done
}

docker_release() {
    for name in "${docker_names[@]}"
    do
        docker push kubedb/$IMG:$TAG-$name
    done
}

docker_check() {
    for i in "${docker_names[@]}"
    do
        echo "Chcking $IMG ..."
        name=$i-$(date +%s | sha256sum | base64 | head -c 8 ; echo)
        docker run -d -P -it --name=$name kubedb/$IMG:$TAG-$i
        docker exec -it $name ps aux
        sleep 5
        docker exec -it $name ps aux
        docker stop $name && docker rm $name
    done
}

binary_repo $@