package controller

import (
	"context"
	"fmt"
	"net"
	"strings"

	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	iistio "github.com/hexiaodai/fence/internal/istio"
	"github.com/hexiaodai/fence/internal/log"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LogEntry struct {
	client.Client
	sidecar        *iistio.Sidecar
	namespaceCache *cache.Namespace
	ipServiceCache *cache.IpService
	config         config.Fence
	log            logr.Logger
}

func NewLogEntry(client client.Client, sidecar *iistio.Sidecar, namespaceCache *cache.Namespace, ipServiceCache *cache.IpService, config config.Fence) *LogEntry {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("LogEntry", "StreamLogEntry")

	return &LogEntry{Client: client, sidecar: sidecar, namespaceCache: namespaceCache, ipServiceCache: ipServiceCache, config: config, log: logger}
}

func (l *LogEntry) StreamLogEntry(logEntrys []*data_accesslog.HTTPAccessLogEntry) {
	for _, entry := range logEntrys {
		nn, err := l.getNamespacedName(entry)
		if err != nil {
			l.log.Error(err, "failed to get sidecar namespaceName")
			continue
		}

		if isSystemNamespace(l.config, nn.Namespace) {
			continue
		}

		l.log.WithName(nn.String()).Info("logEntry stream object")

		svc := &corev1.Service{}
		if err := l.Client.Get(context.Background(), nn, svc); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			l.log.Error(err, "failed to get service", "namespaceName", nn)
			continue
		}

		if !fenceIsEnabled(l.namespaceCache, l.config, svc) {
			continue
		}

		if l.isInternalService(entry) {
			if err := l.refreshSidecar(nn, entry); err != nil {
				l.log.Error(err, "failed to refresh sidecar", "namespaceName", nn)
				continue
			}
		} else {
			if err := l.refreshEnvoyFilter(entry); err != nil {
				l.log.Error(err, "failed to refresh envoyFilter", "namespaceName", nn)
				continue
			}
		}
	}
}

func (l *LogEntry) refreshSidecar(nn types.NamespacedName, entry *data_accesslog.HTTPAccessLogEntry) error {
	log := l.log.WithName("refreshSidecar")

	found := &networkingv1alpha3.Sidecar{}
	if err := l.Client.Get(context.Background(), nn, found); err != nil {
		if errors.IsNotFound(err) {
			log.Info("resource not found. ignoring since object must be deleted", "namespaceName", nn)
			return nil
		}
		return fmt.Errorf("failed to get sidecar, namespaceName %v, error %v", nn, err)
	}

	if err := l.sidecar.AddDestinationSvcToEgress(found, entry); err != nil {
		return fmt.Errorf("failed to add destination service to egress, namespaceName %v, error %v", nn, err)
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return l.Client.Update(context.Background(), found)
	})
	if retryErr != nil {
		return fmt.Errorf("failed to update sidecar, exceeded the maximum number of conflict retries, namespaceName %v, error %v", nn, retryErr)
	}
	return nil
}

func (l *LogEntry) refreshEnvoyFilter(entry *data_accesslog.HTTPAccessLogEntry) error {
	log := l.log.WithName("refreshEnvoyFilter")

	nn := types.NamespacedName{Namespace: l.config.IstioNamespace, Name: "fence-proxy"}
	found := &networkingv1alpha3.EnvoyFilter{}
	if err := l.Client.Get(context.Background(), nn, found); err != nil {
		if errors.IsNotFound(err) {
			log.Info("resource not found. ignoring since object must be deleted", "namespaceName", nn)
			return nil
		}
		return fmt.Errorf("failed to get envoyFilter, namespaceName %v, error %v", nn, err)
	}

	iistio.AddExternalServiceToRouteConfigUration(entry.Request.Authority, found)
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return l.Client.Update(context.Background(), found)
	})
	if retryErr != nil {
		return fmt.Errorf("failed to update envoyFilter, exceeded the maximum number of conflict retries, namespaceName %v, error %v", nn, retryErr)
	}
	return nil
}

func (l *LogEntry) getNamespacedName(entry *data_accesslog.HTTPAccessLogEntry) (out types.NamespacedName, err error) {
	sourceIp, err := l.ipServiceCache.FetchSourceIp(entry)
	if err != nil {
		return
	}
	sourceSvc, err := l.ipServiceCache.FetchSourceSvc(sourceIp)
	if err != nil {
		err = fmt.Errorf("failed to get source service, source ip is %v", sourceIp)
		return
	}
	return types.NamespacedName{Namespace: sourceSvc.Namespace, Name: sourceSvc.Name}, nil
}

func (l *LogEntry) isInternalService(entry *data_accesslog.HTTPAccessLogEntry) bool {
	dest := strings.Split(entry.Request.Authority, ":")[0]
	if dest == "" || net.ParseIP(dest) != nil {
		return false
	}

	destParts := strings.Split(dest, ".")
	if len(destParts) == 0 {
		return false
	}
	destSvc := types.NamespacedName{Name: destParts[0]}
	if len(destParts) == 1 {
		sourceIp, err := l.ipServiceCache.FetchSourceIp(entry)
		if err != nil {
			return false
		}
		sourceSvc, err := l.ipServiceCache.FetchSourceSvc(sourceIp)
		if err != nil {
			return false
		}
		destSvc.Namespace = sourceSvc.Namespace
	} else {
		destSvc.Namespace = destParts[1]
	}

	_, ok := l.ipServiceCache.SvcToIps.Ips[destSvc]
	return ok
}
