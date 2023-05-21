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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NamespaceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
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

	if err := r.createSidecar(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to createSidecar: %v", err)
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) createSidecar(ctx context.Context, ns *corev1.Namespace) error {
	svcs := &corev1.ServiceList{}
	if err := r.Client.List(ctx, svcs, &client.ListOptions{Namespace: ns.Name}); err != nil && !errors.IsNotFound(err) {
		return err
	}

	for _, svc := range svcs.Items {
		if !fenceIsEnabled(ns, r.Config, &svc) {
			continue
		}
		sidecar, err := r.Sidecar.Generate(&svc)
		if err != nil {
			if goerrors.Is(err, istio.ErrNoLabelSelector) {
				continue
			}
			r.log.Error(err, "failed to generate sidecar")
			continue
		}
		if err := ctrl.SetControllerReference(&svc, sidecar, r.Scheme); err != nil {
			r.log.Error(err, "failed to setControllerReference")
			continue
		}
		if err := r.Client.Create(ctx, sidecar); err != nil && !errors.IsAlreadyExists(err) {
			r.log.Error(err, "failed to create sidecar")
			continue
		}
	}
	return nil
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
