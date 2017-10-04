package initializer

import (
	"sync"

	"k8s.io/client-go/pkg/apis/apps/v1beta1"
)

type DeploymentStore struct {
	sync.RWMutex
	table map[string]*v1beta1.Deployment
}

func NewDeploymentStore() *DeploymentStore {
	store := &DeploymentStore{
		table: make(map[string]*v1beta1.Deployment)}
	return store
}

func (store *DeploymentStore) put(key string, value *v1beta1.Deployment) {
	store.Lock()
	store.table[key] = value
	store.Unlock()
}

func (store *DeploymentStore) get(key string) *v1beta1.Deployment {
	store.RLock()
	value := store.table[key]
	store.RUnlock()
	return value
}

func (store *DeploymentStore) deleteKey(key string) {
	store.Lock()
	delete(store.table, key)
	store.Unlock()
}
