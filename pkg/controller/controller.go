package controller

import (
	"context"
	"log"
	"reflect"
	"time"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	crv1 "github.com/python/consul-operator/pkg/crd/v1"
)

const consulCRDName = crv1.ConsulResourcePlural + "." + crv1.GroupName

type consulController struct {
	config *rest.Config
}

func NewController(config *rest.Config) *consulController {
	cc := &consulController{
		config: config,
	}

	return cc
}

func (c *consulController) Run(ctx context.Context) error {
	// Before we do anything else, ensure that our CustomResourceDefinition has
	// been created. Doing this here prevents people from needing to manage
	// this on their own.
	err := c.initResource()
	if err != nil {
		return err
	}

	<-ctx.Done()
	return ctx.Err()
}

func (c *consulController) initResource() error {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: consulCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   crv1.GroupName,
			Version: crv1.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: crv1.ConsulResourcePlural,
				Kind:   reflect.TypeOf(crv1.Consul{}).Name(),
			},
		},
	}

	clientset, err := apiextensionsclient.NewForConfig(c.config)
	if err != nil {
		return err
	}

	_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	// Wait for CRD being established
	err = wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		crd, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(consulCRDName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, err
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					log.Printf("Name conflict: %v", cond.Reason)
				}
			}
		}
		return false, err
	})

	return nil
}
