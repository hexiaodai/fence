package controller

import (
	"context"
	"fmt"

	goerrors "errors"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/istio"
	"github.com/hexiaodai/fence/internal/log"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
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

	if !fenceIsEnabled(r.NamespaceCache, r.Config, instance) {
		return ctrl.Result{}, nil
	}

	if err := r.bindPortToFence(ctx, instance.Spec.Ports); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to bind port, namespaceName %v, %w", request.NamespacedName, err)
	}

	if err := r.createSidecar(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create sidecar, namespaceName %v, %w", request.NamespacedName, err)
	}

	if err := r.updateEnvoyFilter(ctx, instance); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update envoy filter, namespaceName %v, %w", request.NamespacedName, err)
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) createSidecar(ctx context.Context, svc *corev1.Service) error {
	sidecar, err := r.Sidecar.Generate(svc)
	if err != nil {
		if goerrors.Is(err, istio.ErrNoLabelSelector) {
			return nil
		}
		return err
	}
	if err := ctrl.SetControllerReference(svc, sidecar, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(ctx, sidecar); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *ServiceReconciler) updateEnvoyFilter(ctx context.Context, svc *corev1.Service) error {
	envoyFilter := &networkingv1alpha3.EnvoyFilter{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Config.IstioNamespace, Name: "fence-proxy"}, envoyFilter); err != nil {
		return err
	}
	istio.MergeFenceProxyEnvoyFilter(&envoyFilter.Spec, svc)

	return r.Client.Update(ctx, envoyFilter)
}

func (r *ServiceReconciler) bindPortToFence(ctx context.Context, sps []corev1.ServicePort) error {
	fenceProxySvc := &corev1.Service{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.Config.FenceNamespace, Name: "fence-proxy"}, fenceProxySvc); err != nil {
		return err
	}
	indexer := map[int32]struct{}{}
	for _, p := range fenceProxySvc.Spec.Ports {
		indexer[p.Port] = struct{}{}
	}
	for _, p := range sps {
		if p.Protocol != corev1.ProtocolTCP {
			continue
		}
		if _, ok := indexer[p.Port]; ok {
			continue
		}
		sp := corev1.ServicePort{
			Name:       fmt.Sprintf("http-%v", p.Port),
			Protocol:   corev1.ProtocolTCP,
			Port:       p.Port,
			TargetPort: intstr.Parse(r.Config.WormholePort),
		}
		fenceProxySvc.Spec.Ports = append(fenceProxySvc.Spec.Ports, sp)
	}
	return r.Update(ctx, fenceProxySvc)
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}
