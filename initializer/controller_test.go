package initializer

import (
	"testing"

	"github.com/ghodss/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newInitializers() *metav1.Initializers {
	return &metav1.Initializers{
		Pending: []metav1.Initializer{}}
}

func appendInitializer(initializers *metav1.Initializers, initializer metav1.Initializer) *metav1.Initializers {
	initializers.Pending = append(initializers.Pending, initializer)
	return initializers
}

func TestProcessDeployment(t *testing.T) {
	ogaInitializer := &OgaInitializer{
		store:     NewDeploymentStore(),
		clientSet: &fake.Clientset{},
		stop:      make(chan struct{}),
	}
	deployment := NewDeployment("foo")
	deployment.ObjectMeta.Initializers = newInitializers()
	deployment.ObjectMeta.Initializers = appendInitializer(newInitializers(),
		metav1.Initializer{Name: "dummyInitializer.k8s.io"})

	req := &FakeRequester{Name: "fake"}
	ogaInitializer.processDeployment(deployment, req)

	if ogaInitializer.store.get("foo") != nil {
		t.Errorf("processDeployment added 'foo' to the DeploymentStore when it" +
			"shouldn't")
	}

	deployment.ObjectMeta.Initializers = appendInitializer(newInitializers(),
		metav1.Initializer{Name: defaultInitializerName})
	deployment.ObjectMeta.Annotations = map[string]string{defaultAnnotation: `fake:
    approvers:
      - foo
      - bar
    channel: #baz`}

	ogaInitializer.processDeployment(deployment, req)

	if ogaInitializer.store.get("foo") == nil {
		t.Errorf("processDeployment should have added 'foo' to the" +
			"DeploymentStore but didn't")
	}
	ogaInitializer.store.deleteKey("foo")
}

func TestInitializeDeployment(t *testing.T) {
	ogaInitializer := &OgaInitializer{
		store:     NewDeploymentStore(),
		clientSet: &fake.Clientset{},
		stop:      make(chan struct{}),
	}
	deployment := NewDeployment("foo")
	deployment.ObjectMeta.Initializers = newInitializers()
	deployment.ObjectMeta.Initializers = appendInitializer(deployment.ObjectMeta.Initializers,
		metav1.Initializer{Name: "dummyInitializer.k8s.io"})

	deployment.ObjectMeta.Initializers = appendInitializer(deployment.ObjectMeta.Initializers,
		metav1.Initializer{Name: defaultInitializerName})
	deployment.ObjectMeta.Initializers = appendInitializer(deployment.ObjectMeta.Initializers,
		metav1.Initializer{Name: "bar.foo.com"})

	//Should correctly remove the defaultInitializer on the deployment
	ogaInitializer.initializeDeployment(deployment)

	if len(deployment.ObjectMeta.Initializers.Pending) != 2 {
		t.Errorf("initializeDeployment did not remove defaultInitializer\n")
		t.Errorf("%v are the intializers\n", deployment.ObjectMeta.Initializers.Pending)
	}

	expectedInitializers := []string{
		"dummyInitializer.k8s.io",
		"bar.foo.com"}

	for i, initializer := range deployment.ObjectMeta.Initializers.Pending {
		if initializer.Name != expectedInitializers[i] {
			t.Errorf("Initializers updated in wrong order: got %s want %s",
				initializer.Name, expectedInitializers[i])
		}
	}

	//Should not modify deployments that it does not have an initializer on
	ogaInitializer.initializeDeployment(deployment)

	if len(deployment.ObjectMeta.Initializers.Pending) != 2 {
		t.Errorf("initializeDeployment wrongly modified the deployment\n")
		t.Errorf("%v are the intializers\n",
			deployment.ObjectMeta.Initializers.Pending)
	}

	for i, initializer := range deployment.ObjectMeta.Initializers.Pending {
		if initializer.Name != expectedInitializers[i] {
			t.Errorf("Initializers order modified: got %s want %s\n",
				initializer.Name, expectedInitializers[i])
		}
	}
}

func TestApproveDeployment(t *testing.T) {
	ogaInitializer := &OgaInitializer{
		store:     NewDeploymentStore(),
		clientSet: &fake.Clientset{},
		stop:      make(chan struct{}),
	}
	deployment := NewDeployment("foo")
	deployment.ObjectMeta.Initializers = newInitializers()
	deployment.ObjectMeta.Initializers = appendInitializer(deployment.ObjectMeta.Initializers,
		metav1.Initializer{Name: "dummyInitializer.k8s.io"})
	deployment.ObjectMeta.Initializers = appendInitializer(deployment.ObjectMeta.Initializers,
		metav1.Initializer{Name: defaultInitializerName})
	deployment.ObjectMeta.Annotations = map[string]string{defaultAnnotation: "fake:\n" +
		"  approvers:\n" +
		"    - \"@foo\"\n" +
		"    - \"@bar\"\n" +
		"  channel: \"#baz\"\n"}
	ogaInitializer.store.put("foo", deployment)

	ogaInitializer.ApproveDeployment(&Approval{
		Approver:       "@bjhaid",
		DeploymentName: "foo",
		RequesterName:  "fake",
	})

	if ogaInitializer.store.get("foo") != nil {
		t.Errorf("ApprovedDeployment should have removed deployment 'foo' from" +
			"the DeploymentStore but didn't\n")
	}

	actualAnnotation := make(map[string]interface{})
	err :=
		yaml.Unmarshal([]byte(deployment.ObjectMeta.Annotations[defaultAnnotation]),
			&actualAnnotation)

	if err != nil {
		t.Errorf("Error from unmarshalling annotation: %s", err)
	}

	if actualAnnotation["approved_by"] != "@bjhaid" {
		t.Errorf("ApprovedDeployment should have added 'approved_by' field to the" +
			"the defaultAnnotation but didn't\n")
		t.Errorf("ActualAnnotation: %v\n", actualAnnotation)
	}
}
