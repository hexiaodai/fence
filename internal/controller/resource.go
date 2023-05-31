package controller

import (
	"context"
	goerrors "errors"
	"fmt"

	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	iistio "github.com/hexiaodai/fence/internal/istio"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Resource struct {
	client.Client
	scheme         *runtime.Scheme
	config         config.Fence
	sidecar        *iistio.Sidecar
	namespaceCache *cache.Namespace
}

func NewResource(client client.Client, sidecar *iistio.Sidecar, namespaceCache *cache.Namespace, config config.Fence, scheme *runtime.Scheme) *Resource {
	return &Resource{Client: client, sidecar: sidecar, namespaceCache: namespaceCache, config: config, scheme: scheme}
}

func (r *Resource) Refresh(ctx context.Context, obj interface{}) error {
	switch v := obj.(type) {
	case *corev1.Service:
		svc := obj.(*corev1.Service)
		nn := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
		if err := r.BindPortToFence(ctx, svc.Spec.Ports); err != nil {
			if errors.IsConflict(err) {
				return err
			}
			return fmt.Errorf("failed to bind port, namespaceName %v, %w", nn, err)
		}
		if err := r.CreateSidecar(ctx, svc); err != nil {
			return fmt.Errorf("failed to create sidecar, namespaceName %v, %w", nn, err)
		}
		if err := r.AddServiceToEnvoyFilter(ctx, svc); err != nil {
			if errors.IsConflict(err) {
				return err
			}
			return fmt.Errorf("failed to update envoy filter, namespaceName %v, %w", nn, err)
		}
	case *HTTPAccessLogEntryWrapper:
		entry := obj.(*HTTPAccessLogEntryWrapper)
		if entry.DestinationService == Internal {
			if err := r.AddDestinationServiceToSidecar(entry); err != nil {
				return fmt.Errorf("failed to add destination service to sidecar, namespaceName: %v, error: %w", entry.NamespacedName, err)
			}
		}
		if entry.DestinationService == External {
			if err := r.AddExternalServiceToEnvoyFilter(entry); err != nil {
				return fmt.Errorf("failed to add external service to envoyFilter, namespaceName: %v, error: %w", entry.NamespacedName, err)
			}
		}
	default:
		return fmt.Errorf("unknown type %v", v)
	}

	return nil
}

func (r *Resource) CreateSidecar(ctx context.Context, svc *corev1.Service) error {
	sidecar, err := r.sidecar.Generate(svc)
	if err != nil {
		if goerrors.Is(err, iistio.ErrNoLabelSelector) {
			return nil
		}
		return err
	}
	if err := ctrl.SetControllerReference(svc, sidecar, r.scheme); err != nil {
		return err
	}
	if err := r.Client.Create(context.Background(), sidecar); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *Resource) AddDestinationServiceToSidecar(entry *HTTPAccessLogEntryWrapper) error {
	found := &networkingv1alpha3.Sidecar{}
	if err := r.Client.Get(context.Background(), entry.NamespacedName, found); err != nil {
		if errors.IsNotFound(err) {
			// TODO: create sidecar
			// log.Info("resource not found. ignoring since object must be deleted", "namespaceName", nn)
			return nil
		}
		return fmt.Errorf("failed to get sidecar, namespaceName %v, error %v", entry.Namespace, err)
	}

	if err := r.sidecar.AddDestinationSvcToEgress(found, entry.HTTPAccessLogEntry); err != nil {
		return fmt.Errorf("failed to add destination service to egress, namespaceName %v, error %v", entry.Namespace, err)
	}
	return r.Client.Update(context.Background(), found)
}

func (r *Resource) AddServiceToEnvoyFilter(ctx context.Context, svc *corev1.Service) error {
	envoyFilter := &networkingv1alpha3.EnvoyFilter{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.config.IstioNamespace, Name: "fence-proxy"}, envoyFilter); err != nil {
		return err
	}
	iistio.MergeFenceProxyEnvoyFilter(&envoyFilter.Spec, svc)
	return r.Client.Update(ctx, envoyFilter)
}

func (r *Resource) AddExternalServiceToEnvoyFilter(entry *HTTPAccessLogEntryWrapper) error {
	nn := types.NamespacedName{Namespace: r.config.IstioNamespace, Name: "fence-proxy"}
	found := &networkingv1alpha3.EnvoyFilter{}
	if err := r.Client.Get(context.Background(), nn, found); err != nil {
		if errors.IsNotFound(err) {
			// log.Info("resource not found. ignoring since object must be deleted", "namespaceName", nn)
			return nil
		}
		return fmt.Errorf("failed to get envoyFilter, namespaceName %v, error %v", nn, err)
	}
	iistio.AddExternalServiceToRouteConfigUration(entry.Request.Authority, found)
	return r.Client.Update(context.Background(), found)
}

func (r *Resource) BindPortToFence(ctx context.Context, sps []corev1.ServicePort) error {
	fenceProxySvc := &corev1.Service{}
	if err := r.Client.Get(context.Background(), types.NamespacedName{Namespace: r.config.FenceNamespace, Name: "fence-proxy"}, fenceProxySvc); err != nil {
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
			TargetPort: intstr.Parse(r.config.WormholePort),
		}
		fenceProxySvc.Spec.Ports = append(fenceProxySvc.Spec.Ports, sp)
	}
	return r.Client.Update(context.Background(), fenceProxySvc)
}
