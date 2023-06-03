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

type EndpointsReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Config         config.Fence
	Sidecar        *istio.Sidecar
	NamespaceCache *cache.Namespace
	Resource       *Resource
	log            logr.Logger
}

type EndpointsReconcilerOpts func(*EndpointsReconciler)

func NewEndpointsReconciler(opts ...EndpointsReconcilerOpts) *EndpointsReconciler {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("Reconcile", "Endpoints")

	r := &EndpointsReconciler{
		log: logger,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *EndpointsReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithName(request.NamespacedName.String())

	log.V(5).Info("endpoints reconciling object")

	if isSystemNamespace(r.Config, request.Namespace) {
		log.V(5).Info("skip system namespace")
		return ctrl.Result{}, nil
	}

	instance := &corev1.Endpoints{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			log.V(5).Info("resource not found. ignoring since object must be deleted")
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get endpoints: %v", err)
		}
	}

	if len(instance.Subsets) == 0 {
		log.V(5).Info("subsets are empty")
		return ctrl.Result{}, nil
	}

	svc, pod, err := r.fetchServiceAndPod(ctx, instance)
	if err != nil {
		if goerrors.Is(err, errNotFound) || errors.IsNotFound(err) {
			log.V(5).Info("no service and pod associated")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch service and pod: %v", err)
	}

	if !fenceIsEnabled(r.NamespaceCache, r.Config, pod) || !isInjectSidecar(pod) {
		log.V(5).Info("no fence enabled or no sidecar injected")
		return ctrl.Result{}, nil
	}

	if err := r.Resource.Refresh(ctx, svc); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to refresh resource, namespaceName %v, %w", request.NamespacedName, err)
	}

	return ctrl.Result{}, nil
}

var errNotFound = fmt.Errorf("not found")

func (r *EndpointsReconciler) fetchServiceAndPod(ctx context.Context, ep *corev1.Endpoints) (svc *corev1.Service, pod *corev1.Pod, err error) {
	if len(ep.Subsets) == 0 {
		err = errNotFound
		return
	}
	svc = &corev1.Service{}
	pod = &corev1.Pod{}
	if err = r.Client.Get(ctx, types.NamespacedName{Namespace: ep.Namespace, Name: ep.Name}, svc); err != nil {
		err = fmt.Errorf("failed to get service: %w", err)
		return
	}
	list := &corev1.PodList{}
	if err = r.Client.List(ctx, list, &client.ListOptions{
		LabelSelector: labels.Set(svc.Spec.Selector).AsSelector(),
		Limit:         1,
	}); err != nil {
		err = fmt.Errorf("failed to list pod: %v", err)
		return
	}
	if len(list.Items) == 0 {
		err = errNotFound
		return
	}
	pod = &list.Items[0]
	return
}

func (r *EndpointsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}
