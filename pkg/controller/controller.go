package controller

import (
	"context"
	"log"
	"reflect"
	"time"

	apiv1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	consulclient "github.com/python/consul-operator/pkg/client"
	crv1 "github.com/python/consul-operator/pkg/crd/v1"
)

const consulCRDName = crv1.ConsulResourcePlural + "." + crv1.GroupName

type consulController struct {
	config       *rest.Config
	consulClient *rest.RESTClient
	consulScheme *runtime.Scheme
}

func NewController(config *rest.Config) (*consulController, error) {
	// make a new config for our extension's API group, using the first config
	// as a baseline
	consulClient, consulScheme, err := consulclient.NewClient(config)
	if err != nil {
		return nil, err
	}

	c := &consulController{
		config:       config,
		consulClient: consulClient,
		consulScheme: consulScheme,
	}

	return c, nil
}

func (c *consulController) Run(ctx context.Context) error {
	// Before we do anything else, ensure that our CustomResourceDefinition has
	// been created. Doing this here prevents people from needing to manage
	// this on their own.
	err := c.initResource()
	if err != nil {
		return err
	}

	// Start watching for changes in our resources, this is how we'll keep track
	// of what clusters we expect there to exist, and what configuration they
	// should be in.
	err = c.watchResource(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()
	return ctx.Err()
}

func (c *consulController) watchResource(ctx context.Context) error {
	source := cache.NewListWatchFromClient(
		c.consulClient,
		crv1.ConsulResourcePlural,
		apiv1.NamespaceAll,
		fields.Everything())

	_, controller := cache.NewInformer(
		source,
		&crv1.Consul{},
		20*time.Second,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.onAdd,
			UpdateFunc: c.onUpdate,
			DeleteFunc: c.onDelete,
		})

	go controller.Run(ctx.Done())

	return nil
}

func (c *consulController) onAdd(obj interface{}) {
	consul := obj.(*crv1.Consul)

	log.Printf("[CONTROLLER] onAdd %v", consul.ObjectMeta.SelfLink)
}

func (c *consulController) onUpdate(oldObj, newObj interface{}) {
	oldConsul := oldObj.(*crv1.Consul)
	newConsul := newObj.(*crv1.Consul)

	log.Printf("[CONTROLLER] OnUpdate newObj: %v", oldConsul.ObjectMeta.SelfLink)
	log.Printf("[CONTROLLER] OnUpdate oldObj: %v", newConsul.ObjectMeta.SelfLink)
}

func (c *consulController) onDelete(obj interface{}) {
	consul := obj.(*crv1.Consul)

	log.Printf("[CONTROLLER] onDelete %v", consul.ObjectMeta.SelfLink)
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
