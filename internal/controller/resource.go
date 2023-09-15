package controller

import (
	"context"
	goerrors "errors"
	"fmt"
	"reflect"

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
	config.Server
	client.Client
	scheme         *runtime.Scheme
	sidecar        *iistio.Sidecar
	namespaceCache *cache.Namespace
}

func NewResource(client client.Client, sidecar *iistio.Sidecar, namespaceCache *cache.Namespace, server config.Server, scheme *runtime.Scheme) *Resource {
	server.Logger = server.Logger.WithName("Refresh").WithValues("controller", "Resource")
	return &Resource{
		Client:         client,
		sidecar:        sidecar,
		namespaceCache: namespaceCache,
		Server:         server,
		scheme:         scheme,
	}
}

func (r *Resource) RefreshByService(ctx context.Context, obj *corev1.Service) error {
	nn := types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}.String()
	r.Logger.Sugar().Debugw("refreshing resources through Service", "function", "RefreshByService", "namespaceName", nn)
	if err := r.BindPortToFence(ctx, obj.Spec.Ports); err != nil {
		if errors.IsConflict(err) {
			return err
		}
		return fmt.Errorf("failed to bind port. namespaceName %v. %w", nn, err)
	}
	if err := r.CreateSidecar(ctx, obj); err != nil {
		return fmt.Errorf("failed to create sidecar. namespaceName %v. %w", nn, err)
	}
	if err := r.AddServiceToEnvoyFilter(ctx, obj); err != nil {
		if errors.IsConflict(err) {
			return err
		}
		return fmt.Errorf("failed to update envoy filter. namespaceName %v. %w", nn, err)
	}
	r.Logger.Sugar().Debugw("refreshing resources successfully", "function", "RefreshByService", "namespaceName", nn)
	return nil
}

func (r *Resource) RefreshByHTTPAccessLogEntryWrapper(ctx context.Context, obj *HTTPAccessLogEntryWrapper) error {
	nn := obj.NamespacedName.String()
	r.Logger.Sugar().Debugw("refreshing resources through HTTPAccessLog", "function", "RefreshByHTTPAccessLogEntryWrapper", "namespaceName", nn)
	if obj.DestinationService == Internal {
		if err := r.AddDestinationServiceToSidecar(obj); err != nil {
			return fmt.Errorf("failed to add destination service to sidecar. namespaceName: %v. %w", nn, err)
		}
	} else if obj.DestinationService == External {
		if err := r.AddExternalServiceToEnvoyFilter(obj); err != nil {
			return fmt.Errorf("failed to add external service to envoyFilter. namespaceName: %v. %w", nn, err)
		}
	} else {
		r.Logger.Sugar().Warnw("unknown DestinationService", "function", "RefreshByHTTPAccessLogEntryWrapper", "namespaceName", nn)
	}
	r.Logger.Sugar().Debugw("refreshing resources successfully", "function", "RefreshByHTTPAccessLogEntryWrapper", "namespaceName", nn)
	return nil
}

func (r *Resource) CreateSidecar(ctx context.Context, svc *corev1.Service) error {
	nn := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
	log := r.Logger.WithName(nn.String()).WithValues("function", "CreateSidecar")

	sidecar, err := r.sidecar.Generate(svc)
	if err != nil {
		if goerrors.Is(err, iistio.ErrNoLabelSelector) {
			log.Sugar().Warnw("skip create sidecar", "namespaceName", nn, "error", err)
			return nil
		}
		return err
	}
	if err := ctrl.SetControllerReference(svc, sidecar, r.scheme); err != nil {
		return err
	}
	if err := r.Client.Create(context.Background(), sidecar); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Sugar().Warnw("skip create sidecar", "namespaceName", nn, "error", err)
			return nil
		}
		return err
	}
	log.Sugar().Debugw("create sidecar successfully", "function", "CreateSidecar", "namespaceName", nn)
	return nil
}

func (r *Resource) AddDestinationServiceToSidecar(entry *HTTPAccessLogEntryWrapper) error {
	log := r.Logger.WithName(entry.NamespacedName.String()).WithValues("function", "AddDestinationServiceToSidecar")

	found := &networkingv1alpha3.Sidecar{}
	if err := r.Client.Get(context.Background(), entry.NamespacedName, found); err != nil {
		if errors.IsNotFound(err) {
			log.Sugar().Warnw("skip add destination to sidecar", "namespaceName", entry.NamespacedName, "error", err)
			return nil
		}
		return fmt.Errorf("failed to get sidecar. namespaceName %v. %w", entry.NamespacedName, err)
	}

	if err := r.sidecar.AddDestinationSvcToEgress(found, entry.HTTPAccessLogEntry); err != nil {
		return fmt.Errorf("failed to add destination service to egress. namespaceName %v. %w", entry.NamespacedName, err)
	}
	if err := r.Client.Update(context.Background(), found); err != nil {
		return err
	}
	log.Sugar().Debugw("destination added successfully to sidecar", "function", "AddDestinationServiceToSidecar", "namespaceName", entry.NamespacedName)
	return nil
}

func (r *Resource) AddServiceToEnvoyFilter(ctx context.Context, svc *corev1.Service) error {
	nn := types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}
	log := r.Logger.WithName(nn.String()).WithValues("function", "AddServiceToEnvoyFilter")

	envoyFilter := &networkingv1alpha3.EnvoyFilter{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: r.IstioNamespace, Name: "fence-proxy"}, envoyFilter); err != nil {
		return err
	}
	iistio.MergeFenceProxyEnvoyFilter(&envoyFilter.Spec, svc)
	if err := r.Client.Update(ctx, envoyFilter); err != nil {
		return err
	}
	log.Sugar().Debugw("service added successfully to envoyFilter", "function", "AddServiceToEnvoyFilter", "namespaceName", nn)
	return nil
}

func (r *Resource) AddExternalServiceToEnvoyFilter(entry *HTTPAccessLogEntryWrapper) error {
	nn := types.NamespacedName{Namespace: r.IstioNamespace, Name: "fence-proxy"}
	log := r.Logger.WithName(nn.String()).WithValues("function", "AddExternalServiceToEnvoyFilter")

	found := &networkingv1alpha3.EnvoyFilter{}
	if err := r.Client.Get(context.Background(), nn, found); err != nil {
		if errors.IsNotFound(err) {
			log.Sugar().Warnw("skip add external service to envoyFilter", "namespaceName", nn, "error", err)
			return nil
		}
		return fmt.Errorf("failed to get envoyFilter. namespaceName %v. %w", nn.String(), err)
	}
	iistio.AddExternalServiceToRouteConfigUration(entry.Request.Authority, found)
	if err := r.Client.Update(context.Background(), found); err != nil {
		return err
	}
	log.Sugar().Debugw("external service added successfully to envoyFilter", "function", "AddExternalServiceToEnvoyFilter", "namespaceName", nn)
	return nil
}

func (r *Resource) BindPortToFence(ctx context.Context, sps []corev1.ServicePort) error {
	nn := types.NamespacedName{Namespace: r.FenceNamespace, Name: "fence-proxy"}
	log := r.Logger.WithName(nn.String()).WithValues("function", "BindPortToFence")

	fenceProxySvc := &corev1.Service{}
	if err := r.Client.Get(context.Background(), nn, fenceProxySvc); err != nil {
		return err
	}
	newsps := []corev1.ServicePort{}
	indexer := map[int32]struct{}{}
	for _, p := range fenceProxySvc.Spec.Ports {
		newsps = append(newsps, p)
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
			TargetPort: intstr.Parse(r.WormholePort),
		}
		newsps = append(newsps, sp)
	}
	if reflect.DeepEqual(newsps, fenceProxySvc.Spec.Ports) {
		log.Sugar().Debugw("skip bind port to fence. no port bind required", "namespaceName", nn)
		return nil
	}
	fenceProxySvc.Spec.Ports = newsps

	if err := r.Client.Update(context.Background(), fenceProxySvc); err != nil {
		return err
	}
	log.Sugar().Debugw("ports bind successfully to fence", "function", "BindPortToFence", "namespaceName", nn)
	return nil
}
