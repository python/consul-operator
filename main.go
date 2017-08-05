package main

import (
	"flag"
	"log"

	// Our cluster runs ontop of GKE, so we need to add this. I don't know if
	// there is a better way to handle this for people whose clusters are not
	// on GKE.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	consulclient "github.com/python/consul-operator/pkg/client"
	"github.com/python/consul-operator/pkg/utils/k8sutils"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "Path to a kube config. Only required if out-of-cluster.")
	flag.Parse()

	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	config, err := k8sutils.BuildConfig(*kubeconfig)
	if err != nil {
		log.Fatalf("Error configuring: %v", err)
	}

	// Before we do anything else, ensure that our CustomResourceDefinition has
	// been created. Doing this here prevents people from needing to manage
	// this on their own.
	_, err = consulclient.CreateCustomResourceDefinition(config)
	if err != nil {
		log.Fatalf("Error registering %v: %v", consulclient.ConsulCRDName, err)
	}
}
