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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NamespaceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
	Resource       *Resource
	log            logr.Logger
}

type NamespaceReconcilerOpts func(*NamespaceReconciler)

func NewNamespaceReconciler(opts ...NamespaceReconcilerOpts) *NamespaceReconciler {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("Reconcile", "Namespace")

	r := &NamespaceReconciler{
		log: logger,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	if isSystemNamespace(r.Config, request.Name) {
		return ctrl.Result{}, nil
	}
	r.log.WithName(request.Name).Info("namespace reconciling object", "name", request.Name)

	instance := &corev1.Namespace{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("resource not found. ignoring since object must be deleted", "namesapceName", request.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get namespace: %v", err)
		}
	}

	svcList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, svcList, &client.ListOptions{Namespace: instance.Name}); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	for _, svc := range svcList.Items {
		nn := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
		pod, err := r.fetchPod(ctx, &svc)
		if err != nil {
			if !goerrors.Is(err, errFetchPodNotFound) && !errors.IsNotFound(err) {
				r.log.Error(err, "failed to fetch pod", "namespaceName", nn)
			}
			continue
		}
		if !fenceIsEnabled(r.NamespaceCache, r.Config, pod) || !isInjectSidecar(pod) {
			continue
		}

		if err := r.Resource.Refresh(ctx, &svc); err != nil {
			if errors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) fetchPod(ctx context.Context, svc *corev1.Service) (*corev1.Pod, error) {
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

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
