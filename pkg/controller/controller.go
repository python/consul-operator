package controller

import (
	"context"
	"errors"
	"log"
	"reflect"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	multierror "github.com/hashicorp/go-multierror"

	consulclient "github.com/python/consul-operator/pkg/client"
	crv1 "github.com/python/consul-operator/pkg/crd/v1"
)

const consulCRDName = crv1.ConsulResourcePlural + "." + crv1.GroupName

type consulController struct {
	config       *rest.Config
	client       *kubernetes.Clientset
	consulClient *rest.RESTClient
	consulScheme *runtime.Scheme
	queue        workqueue.RateLimitingInterface
	indexer      cache.Indexer
}

func NewController(config *rest.Config) (*consulController, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// make a new config for our extension's API group, using the first config
	// as a baseline
	consulClient, consulScheme, err := consulclient.NewClient(config)
	if err != nil {
		return nil, err
	}

	c := &consulController{
		config:       config,
		client:       client,
		consulClient: consulClient,
		consulScheme: consulScheme,
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
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

	indexer, informer := cache.NewIndexerInformer(
		source,
		&crv1.Consul{},
		// 20*time.Second,
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if key, err := cache.MetaNamespaceKeyFunc(obj); err == nil {
					c.queue.Add(key)
				}
			},
			UpdateFunc: func(old, new interface{}) {
				if key, err := cache.MetaNamespaceKeyFunc(new); err == nil {
					c.queue.Add(key)
				}
			},
			DeleteFunc: func(obj interface{}) {
				if key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err == nil {
					c.queue.Add(key)
				}
			},
		}, cache.Indexers{})

	c.indexer = indexer

	go informer.Run(ctx.Done())

	// Wait for all caches to be synced, before processing items from the queue
	// is started
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
		return errors.New("Timed out waiting for resoure caches to sync")
	}

	// Actually run our worker, which will pull items off the queue and process
	// them.
	go wait.Until(c.runWorker, time.Second, ctx.Done())

	return nil
}

func (c *consulController) runWorker() {
	for c.processNext() {
	}
}

func (c *consulController) processNext() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}

	// Tell the queue that we are done with processing this key. This unblocks
	// the key for other workers This allows safe parallel processing because
	// two pods with the same key are never processed in parallel.
	defer c.queue.Done(key)

	err := c.process(key.(string))
	if err != nil {
		// This controller retries 5 times if something goes wrong. After that,
		// it stops trying.
		if c.queue.NumRequeues(key) < 5 {
			log.Printf("Error syncing cluster %v: %v", key, err)

			// Re-enqueue the key rate limited. Based on the rate limiter on
			// the queue and the re-enqueue history, the key will be processed
			// later again.
			c.queue.AddRateLimited(key)
		} else {
			log.Printf("Giving up syncing cluster %v: %v", key, err)

			// Forget about the #AddRateLimited history of the key on every
			// successful synchronization. This ensures that future processing
			// of updates for this key is not delayed because of an outdated
			// error history.
			c.queue.Forget(key)
		}
	} else {
		// Again, forgetting the AddRateLimited history to prevent an outdated
		// error history.
		c.queue.Forget(key)
	}

	return true
}

func (c *consulController) process(key string) error {
	log.Printf("Process: %v", key)

	obj, exists, err := c.indexer.GetByKey(key)
	if err != nil {
		log.Printf("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	if exists {
		return c.syncConsul(obj.(*crv1.Consul))
	} else {
		namespace := strings.Split(key, "/")[0]
		name := strings.Split(key, "/")[1]
		return c.deleteConsul(namespace, name)
	}
}

func (c *consulController) syncConsul(consul *crv1.Consul) error {
	err := c.syncService(consul)
	if err != nil {
		log.Printf("Syncing service for %v failed: %v", consul.GetName(), err)
		return err
	}

	return nil
}

func (c *consulController) deleteConsul(namespace, name string) error {
	var result *multierror.Error

	err := c.deleteService(namespace, name)
	if err != nil {
		log.Printf("Error deleting service for %v/%v", namespace, name)
		result = multierror.Append(result, err)
	}

	return result.ErrorOrNil()
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
