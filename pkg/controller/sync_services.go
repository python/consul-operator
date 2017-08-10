package controller

import (
	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crv1 "github.com/python/consul-operator/pkg/crd/v1"
)

func (c *consulController) syncService(consul *crv1.Consul) error {
	serviceClient := c.client.CoreV1().Services(consul.GetNamespace())

	// Attempt to fetch our serviceDef from the kuberentes server
	_, err := serviceClient.Get(consul.GetName(), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		_, err = serviceClient.Create(&apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      consul.GetName(),
				Namespace: consul.GetNamespace(),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(consul, crv1.SchemeConsulGroupVersionKind),
				},
				Annotations: map[string]string{
					"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
				},
			},
			Spec: apiv1.ServiceSpec{
				Ports: []apiv1.ServicePort{
					{Name: "server-rpc", Port: 8300},
					{Name: "serf-lan", Port: 8301},
					{Name: "serf-wan", Port: 8302},
					{Name: "http-api", Port: 8500},
					{Name: "dns-api", Port: 8600},
				},
				Selector:  map[string]string{"consul-cluster": consul.GetName()},
				ClusterIP: "None",
			},
		})
		if err != nil {
			return err
		}
	} else {
		// TODO: Implementing Updating of Services? This doesn't really
		//       make any sense right now, because there's nothing that
		//       can be updated. However if we let people configure ports
		//       then we'll want to implement this.
	}

	return nil
}

func (c *consulController) deleteService(namespace, name string) error {
	serviceClient := c.client.CoreV1().Services(namespace)

	err := serviceClient.Delete(name, &metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}
