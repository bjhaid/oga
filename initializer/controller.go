package initializer

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	annotation      string
	initializerName string
	kubeconfig      *string
)

type Approval struct {
	Approver       string
	DeploymentName string
	RequesterName  string
}

type Requester interface {
	RequestApproval(Interface, *Approval, string)
	GetName() string
}

type Interface interface {
	GetDeployment(deploymentName string) *v1beta1.Deployment
	ApproveDeployment(approvedDeployment *Approval)
}

type OgaInitializer struct {
	clientSet kubernetes.Interface
	store     *DeploymentStore
	stop      chan struct{}
}

const (
	defaultInitializerName = "oga.initializer.kubernetes.io"
	defaultAnnotation      = "initializer.kubernetes.io/oga"
)

func init() {
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube",
			"config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the "+
			"kubeconfig file")
	}
	flag.StringVar(&annotation, "annotation", defaultAnnotation, "The "+
		"annotation to trigger initialization")
	flag.StringVar(&initializerName, "initializer-name", defaultInitializerName,
		"The initializer name")
}

func Run(stop chan struct{}, req Requester) {
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			glog.Fatalf("Failed loading kubeconfig %s", err)
		}
	}

	var clientset kubernetes.Interface

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("%s", err)
	}

	restClient := clientset.AppsV1beta1().RESTClient()
	watchlist := cache.NewListWatchFromClient(restClient, "deployments",
		corev1.NamespaceAll, fields.Everything())
	ogaInitializer := &OgaInitializer{
		store:     NewDeploymentStore(),
		clientSet: clientset,
		stop:      stop,
	}
	// Wrap the returned watchlist to workaround the inability to include the
	// `IncludeUninitialized` list option when setting up watch clients.
	includeUninitializedWatchlist := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.IncludeUninitialized = true
			return watchlist.List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.IncludeUninitialized = true
			return watchlist.Watch(options)
		},
	}

	resyncPeriod := 30 * time.Second

	_, controller := cache.NewInformer(includeUninitializedWatchlist,
		&v1beta1.Deployment{}, resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				deployment := obj.(*v1beta1.Deployment)
				glog.V(5).Infof("Processing deployment %v", deployment)
				ogaInitializer.processDeployment(deployment, req)
			},
		},
	)
	go controller.Run(stop)
}

func (oga *OgaInitializer) ApproveDeployment(approvedDeployment *Approval) {
	deployment := oga.store.get(approvedDeployment.DeploymentName)
	actualAnnotation := make(map[string]interface{})
	glog.V(2).Infof("Deployment: %s was approved by: %s, initializing",
		approvedDeployment.DeploymentName, approvedDeployment.Approver)

	err :=
		yaml.Unmarshal([]byte(deployment.ObjectMeta.Annotations[defaultAnnotation]),
			&actualAnnotation)
	if err != nil {
		glog.Errorf("Failed unmarshalling annotation: %s, due to: %s\n",
			deployment.ObjectMeta.Annotations[defaultAnnotation], err)
	} else {
		actualAnnotation["approved_by"] = approvedDeployment.Approver
		b, err := yaml.Marshal(actualAnnotation)
		if err != nil {
			glog.Errorf("Failed marshalling annotation: %v, due to: %s\n",
				annotation, err)
		} else {
			deployment.ObjectMeta.Annotations[defaultAnnotation] = string(b)
		}
	}

	err = oga.initializeDeployment(deployment)
	if err != nil {
		glog.Errorf("Failed initializing deployment: %s, due to: %s",
			approvedDeployment.DeploymentName, err)
	} else {
		glog.V(2).Infof("Completed initializing deployment: %s",
			approvedDeployment.DeploymentName)
	}
	oga.store.deleteKey(approvedDeployment.DeploymentName)
}

func (oga *OgaInitializer) GetDeployment(
	deploymentName string) *v1beta1.Deployment {
	return oga.store.get(deploymentName)
}

func (oga *OgaInitializer) processDeployment(
	deployment *v1beta1.Deployment, req Requester) {
	annotations := deployment.ObjectMeta.GetAnnotations()
	annon, ok := annotations[annotation]
	if deployment.ObjectMeta.GetInitializers() == nil {
		glog.V(2).Infof("Skipping deployment: %s it has already been "+
			"initialized", deployment.Name)
		return
	}
	if ok {
		oga.store.put(deployment.Name, deployment)
		glog.V(3).Infof("Sending request for approval")
		req.RequestApproval(oga, &Approval{
			DeploymentName: deployment.Name,
			RequesterName:  req.GetName(),
		}, annon)
	} else {
		glog.V(2).Infof("Initializing %s due to no matching annotation", deployment.Name)
		//if there's no annotation matching oga's initialize the deployment
		err := oga.initializeDeployment(deployment)
		if err != nil {
			glog.Errorf("Failed initializing deployment: %s, due to: %s",
				deployment.Name, err)
		}
	}
}

func (oga *OgaInitializer) initializeDeployment(
	deployment *v1beta1.Deployment) error {
	pendingInitializers := deployment.ObjectMeta.GetInitializers().Pending
	// Create a copy of the pendingInitializers so we can iterate over it and
	// correctly mutate the pendingInitializers while iterating over the dup
	dupPendingInitializers := make([]metav1.Initializer, len(pendingInitializers))
	copy(dupPendingInitializers, pendingInitializers)

	for i, pendingInitializer := range dupPendingInitializers {

		if initializerName == pendingInitializer.Name {
			glog.V(2).Infof("Initializing deployment: %s", deployment.Name)
			if len(pendingInitializers) == 1 {
				deployment.ObjectMeta.Initializers = nil
			} else {
				deployment.ObjectMeta.Initializers.Pending =
					append(pendingInitializers[:i], pendingInitializers[i+1:]...)
			}

			for {
				dClient :=
					oga.clientSet.AppsV1beta1().Deployments(deployment.Namespace)
				if _, err := dClient.Update(deployment); errors.IsConflict(err) {
					// Deployment is modified in the meanwhile, query the latest version
					// and modify the retrieved object.
					glog.V(3).Infof("Encountered conflict while updating %s, retrying",
						deployment.Name)
					deployment, err = dClient.Get(deployment.Name, metav1.GetOptions{})
				} else if err != nil {
					return err
				} else {
					break
				}
			}

			glog.V(2).Infof("%s initialization completed", deployment.Name)
			break
		}
	}

	return nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
