package controller

import (
	"context"
	goerrors "errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/istio"
	"github.com/hexiaodai/fence/internal/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
	Resource       *Resource
	log            logr.Logger
}

type ServiceReconcilerOpts func(*ServiceReconciler)

func NewServiceReconciler(opts ...ServiceReconcilerOpts) *ServiceReconciler {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("Reconcile", "Service")

	r := &ServiceReconciler{
		log: logger,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *ServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	if isSystemNamespace(r.Config, request.Namespace) {
		return ctrl.Result{}, nil
	}

	r.log.WithName(request.Name).Info("service reconciling object", "namespaceName", request.NamespacedName)

	instance := &corev1.Service{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("resource not found. ignoring since object must be deleted", "namespaceName", request.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get service: %v", err)
		}
	}

	pod, err := r.fetchPod(ctx, instance)
	if err != nil {
		if goerrors.Is(err, errFetchPodNotFound) || errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch pod: %v", err)
	}

	if !fenceIsEnabled(r.NamespaceCache, r.Config, pod) {
		return ctrl.Result{}, nil
	}

	if err := r.Resource.Refresh(ctx, instance); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to refresh resource, namespaceName %v, %w", request.NamespacedName, err)
	}

	return ctrl.Result{}, nil
}

var errFetchPodNotFound = fmt.Errorf("not found")

func (r *ServiceReconciler) fetchPod(ctx context.Context, svc *corev1.Service) (*corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		LabelSelector: labels.Set(svc.Labels).AsSelector(),
		Limit:         1,
	}); err != nil {
		return nil, fmt.Errorf("failed to list pod: %v", err)
	}
	if len(list.Items) == 0 {
		return nil, errFetchPodNotFound
	}
	return &list.Items[0], nil
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}
