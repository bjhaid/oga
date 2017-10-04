package initializer

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
)

func NewDeployment(name string) *v1beta1.Deployment {
	d := v1beta1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{
			UID:         uuid.NewUUID(),
			Name:        name,
			Namespace:   metav1.NamespaceDefault,
			Annotations: make(map[string]string),
		},
		Spec: v1beta1.DeploymentSpec{
			Strategy: v1beta1.DeploymentStrategy{
				Type: v1beta1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &v1beta1.RollingUpdateDeployment{
					MaxUnavailable: func() *intstr.IntOrString { i := intstr.FromInt(0); return &i }(),
					MaxSurge:       func() *intstr.IntOrString { i := intstr.FromInt(0); return &i }(),
				},
			},
			Replicas: func() *int32 { i := int32(1); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: make(map[string]string)},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: make(map[string]string),
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: "foo/bar",
						},
						{
							Image: "bar/baz",
						},
					},
				},
			},
			RevisionHistoryLimit: func() *int32 { i := int32(1); return &i }(),
		},
	}
	return &d
}
