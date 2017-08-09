package main

import (
	"context"
	"flag"
	"log"

	// Our cluster runs ontop of GKE, so we need to add this. I don't know if
	// there is a better way to handle this for people whose clusters are not
	// on GKE.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/python/consul-operator/pkg/controller"
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

	controller, err := controller.NewController(config)
	if err != nil {
		log.Fatalf("Error creating controller: %v", err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	err = controller.Run(ctx)
	if err != nil {
		log.Fatalf("Error running controller: %v", err)
	}
}
