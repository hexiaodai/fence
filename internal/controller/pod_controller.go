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

type PodReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
	Resource       *Resource
	log            logr.Logger
}

type PodReconcilerOpts func(*PodReconciler)

func NewPodReconciler(opts ...PodReconcilerOpts) *PodReconciler {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("Reconcile", "Pod")

	r := &PodReconciler{
		log: logger,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *PodReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	if isSystemNamespace(r.Config, request.Namespace) {
		return ctrl.Result{}, nil
	}

	r.log.WithName(request.Name).Info("pod reconciling object", "namespaceName", request.NamespacedName)

	instance := &corev1.Pod{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("resource not found. ignoring since object must be deleted", "namespaceName", request.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get pod: %v", err)
		}
	}

	if !fenceIsEnabled(r.NamespaceCache, r.Config, instance) {
		return ctrl.Result{}, nil
	}

	svc, err := r.fetchService(ctx, instance)
	if err != nil {
		if goerrors.Is(err, errFetchServiceNotFound) || errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch service: %v", err)
	}

	if err := r.Resource.Refresh(ctx, svc); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to refresh resource, namespaceName %v, %w", request.NamespacedName, err)
	}

	return ctrl.Result{}, nil
}

var errFetchServiceNotFound = fmt.Errorf("not found")

func (r *PodReconciler) fetchService(ctx context.Context, pod *corev1.Pod) (*corev1.Service, error) {
	list := &corev1.ServiceList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		LabelSelector: labels.Set(pod.Labels).AsSelector(),
		Limit:         1,
	}); err != nil {
		return nil, fmt.Errorf("failed to list service: %v", err)
	}
	if len(list.Items) == 0 {
		return nil, errFetchServiceNotFound
	}
	return &list.Items[0], nil
}

func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}
