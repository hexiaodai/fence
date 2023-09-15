package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

func NewNamespace(server config.Server) *Namespace {
	server.Logger = server.Logger.WithName("Namespace").WithValues("cache", "Namespace")
	return &Namespace{
		Server:  server,
		Disable: sync.Map{},
		Enabled: sync.Map{},
	}
}

func (ns *Namespace) Start(ctx context.Context) error {
	config, err := options.DefaultConfigFlags.ToRawKubeConfigLoader().ClientConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
		},
	}
	_, controller := cache.NewInformer(lw, &corev1.Namespace{}, 60*time.Second, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { ns.handleNamespaceUpdate(obj) },
		UpdateFunc: func(_, newObj interface{}) { ns.handleNamespaceUpdate(newObj) },
		DeleteFunc: func(obj interface{}) { ns.handleNamespaceDelete(obj) },
	})

	go controller.Run(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), controller.HasSynced) {
		return fmt.Errorf("failed to wait for namespace cache sync")
	}

	ns.Logger.Info("started")
	return nil
}

type Namespace struct {
	// map[namespaceName]struct{}
	Disable sync.Map
	// map[namespaceName]struct{}
	Enabled sync.Map
	config.Server
}

func (ns *Namespace) handleNamespaceUpdate(obj interface{}) {
	nsv, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}
	if nsv.Labels[config.SidecarFenceLabel] == config.SidecarFenceValueDisable {
		ns.SetDisable(nsv.Name)
	}
	if nsv.Labels[config.SidecarFenceLabel] == config.SidecarFenceValueEnabled {
		ns.SetEnabled(nsv.Name)
	}
}

func (ns *Namespace) handleNamespaceDelete(obj interface{}) {
	nsv, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}
	ns.Delete(nsv.Name)
}

func (ns *Namespace) IsDisable(name string) bool {
	_, ok := ns.Disable.Load(name)
	return ok
}

func (ns *Namespace) IsEnabled(name string) bool {
	_, ok := ns.Enabled.Load(name)
	return ok
}

func (ns *Namespace) SetDisable(name string) {
	ns.Disable.Store(name, struct{}{})
}

func (ns *Namespace) SetEnabled(name string) {
	ns.Enabled.Store(name, struct{}{})
}

func (ns *Namespace) Delete(name string) {
	ns.Disable.Delete(name)
	ns.Enabled.Delete(name)
}
