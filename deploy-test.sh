#!/usr/bin/env bash

function build_server() {
  # GOOS=linux go build -o ./_build/server src/main.go

  repo="miao-coin"
  name="ai-server-test"
  version="01"
  dockerfile="Dockerfile"
  (base_build $repo $name $version $dockerfile)
}

function base_build() {
  repo=$1
  name=$2
  version=$3
  dockerfile=$4

  docker build -t angrymiao/${repo}-${name}:${version} -f $dockerfile .
  docker tag  $(docker image ls -q angrymiao/${repo}-${name}:${version})  registry.cn-shenzhen.aliyuncs.com/angrymiao/${repo}:${name}-${version}
  docker push registry.cn-shenzhen.aliyuncs.com/angrymiao/${repo}:${name}-${version}

  kubectl delete -f deploy/k8s/test/deployment.yaml
  kubectl create -f deploy/k8s/test/deployment.yaml
}


 printf "需要build的项目数字(1 server): "
 read num
#num=$buildType
#echo "build (0 all; 1 schedule; 2 admin; 3 customer; 4 grpc) on num = $num"

if ((num==1)); then
  echo "build server"
  build_server
else
    echo "error"
fi
