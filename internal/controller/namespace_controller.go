package controller

import (
	"context"
	goerrors "errors"
	"fmt"

	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/istio"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NamespaceReconciler struct {
	config.Server
	client.Client
	Scheme         *runtime.Scheme
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
	Resource       *Resource
}

type NamespaceReconcilerOpts func(*NamespaceReconciler)

func NewNamespaceReconciler(opts ...NamespaceReconcilerOpts) *NamespaceReconciler {
	r := &NamespaceReconciler{}
	for _, opt := range opts {
		opt(r)
	}
	r.Logger = r.Logger.WithName("Reconciler").WithValues("controller", "Namespace")
	return r
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.WithValues("namespace", request.Namespace, "name", request.Name)

	if isSystemNamespace(r.Namespace, r.IstioNamespace, request.Name) {
		log.Sugar().Debugw("skip system namespace", "namespaceName", request.NamespacedName)
		return ctrl.Result{}, nil
	}

	instance := &corev1.Namespace{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			log.Sugar().Debugw("resource not found. ignoring since object must be deleted", "namespaceName", request.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get namespace: %v", err)
		}
	}

	if namespaceIsDisable(instance) {
		log.Sugar().Debugw("skip disabled namespace", "namespaceName", request.NamespacedName)
		return ctrl.Result{}, nil
	}

	svcList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, svcList, &client.ListOptions{Namespace: instance.Name}); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	for _, svc := range svcList.Items {
		nn := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
		pod, err := r.fetchPod(ctx, &svc)
		if err != nil {
			if !goerrors.Is(err, errNotFound) && !errors.IsNotFound(err) {
				log.Error(err, "failed to fetch pod", "namespaceName", nn)
			}
			continue
		}
		if !fenceIsEnabled(r.NamespaceCache, r.AutoFence, pod) && !isInjectSidecar(pod) {
			log.Sugar().Debugw("skip namespace without fence enabled or without sidecar injected", "namespaceName", nn)
			continue
		}

		if err := r.Resource.Refresh(ctx, &svc); err != nil {
			if errors.IsConflict(err) {
				log.Sugar().Debugw(err.Error(), "namespaceName", nn)
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) fetchPod(ctx context.Context, svc *corev1.Service) (*corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		LabelSelector: labels.Set(svc.Spec.Selector).AsSelector(),
		Limit:         1,
	}); err != nil {
		return nil, fmt.Errorf("failed to list pod: %v", err)
	}
	if len(list.Items) == 0 {
		return nil, errNotFound
	}
	return &list.Items[0], nil
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
