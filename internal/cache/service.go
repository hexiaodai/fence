package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/log"
	"github.com/hexiaodai/fence/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

func NewService() *Service {
	return &Service{
		NcNameIndexer: make(map[types.NamespacedName]struct{}),
	}
}

func (sc *Service) Start(ctx context.Context) error {
	logger, err := log.NewLogger()
	if err != nil {
		return err
	}
	sc.log = logger.WithValues("cache", "Service")
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
			return client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Services("").Watch(ctx, metav1.ListOptions{})
		},
	}
	_, controller := cache.NewInformer(lw, &corev1.Service{}, 60*time.Second, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { sc.handleServiceUpdate(obj) },
		UpdateFunc: func(_, newObj interface{}) { sc.handleServiceUpdate(newObj) },
		DeleteFunc: func(obj interface{}) { sc.handleServiceDelete(obj) },
	})

	go controller.Run(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), controller.HasSynced) {
		return fmt.Errorf("failed to wait for service cache sync")
	}

	sc.log.Info("started")
	return nil
}

func (sc *Service) handleServiceUpdate(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	nn := types.NamespacedName{
		Name:      svc.Name,
		Namespace: svc.Namespace,
	}

	sc.Set(nn)
}

type Service struct {
	NcNameIndexer map[types.NamespacedName]struct{}
	log logr.Logger
	sync.RWMutex
}

func (sc *Service) handleServiceDelete(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	nn := types.NamespacedName{
		Name:      svc.Name,
		Namespace: svc.Namespace,
	}
	sc.Delete(nn)
}

func (sc *Service) ExistNcName(nn types.NamespacedName) bool {
	sc.RLock()
	defer sc.RUnlock()
	_, ok := sc.NcNameIndexer[nn]
	return ok
}

func (sc *Service) Set(nn types.NamespacedName) {
	sc.Lock()
	defer sc.Unlock()
	sc.NcNameIndexer[nn] = struct{}{}
}

func (sc *Service) Delete(nn types.NamespacedName) {
	sc.Lock()
	defer sc.Unlock()
	delete(sc.NcNameIndexer, nn)
}
