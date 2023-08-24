package controller

import (
	"context"

	icache "github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/istio"
	"github.com/hexiaodai/fence/internal/metric"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	uruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func New(server config.Server) *Runner {
	return &Runner{server}
}

type Runner struct {
	config.Server
}

func (r *Runner) Name() string {
	return "Runner"
}

func (r *Runner) Start(ctx context.Context) error {
	r.Logger = r.Logger.WithName(r.Name()).WithValues("controller", r.Name())

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{
		Development: true,
	})))

	scheme := runtime.NewScheme()
	uruntime.Must(corev1.AddToScheme(scheme))
	uruntime.Must(networkingv1alpha3.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		Port:                    9443,
		LeaderElectionID:        "fence-controller",
		LeaderElection:          true,
		LeaderElectionNamespace: r.Namespace,
	})
	if err != nil {
		r.Logger.Error(err, "start controllers failed")
		return err
	}

	if err := r.registerControllers(mgr); err != nil {
		r.Logger.Error(err, "register controllers failed")
		return err
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			panic(err)
		}
	}()

	r.Logger.Info("started")
	return nil
}

func (r *Runner) registerControllers(mgr ctrl.Manager) error {
	ipService := icache.NewIpService(r.Server)
	if err := ipService.Start(context.Background()); err != nil {
		return err
	}
	namespaceCache := icache.NewNamespace(r.Server)
	if err := namespaceCache.Start(context.Background()); err != nil {
		return err
	}

	sidecar := istio.NewSidecar(ipService, r.Server)

	resource := NewResource(mgr.GetClient(), sidecar, namespaceCache, r.Server, mgr.GetScheme())

	if err := NewEndpointsReconciler(func(sr *EndpointsReconciler) {
		sr.Client = mgr.GetClient()
		sr.Scheme = mgr.GetScheme()
		sr.Sidecar = sidecar
		sr.NamespaceCache = namespaceCache
		sr.Resource = resource
		sr.Server = r.Server
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err := NewNamespaceReconciler(func(nr *NamespaceReconciler) {
		nr.Client = mgr.GetClient()
		nr.Scheme = mgr.GetScheme()
		nr.Sidecar = sidecar
		nr.NamespaceCache = namespaceCache
		nr.Resource = resource
		nr.Server = r.Server
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	metricrunner := metric.New(r.Server)
	if err := metricrunner.Start(context.Background()); err != nil {
		return err
	}
	le := NewLogEntry(mgr.GetClient(), mgr.GetScheme(), sidecar, namespaceCache, ipService, resource, r.Server)
	metricrunner.RegisterHttpLogEntry(le)

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	return nil
}
