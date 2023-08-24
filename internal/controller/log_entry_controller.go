package controller

import (
	"context"
	"fmt"
	"net"
	"strings"

	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	iistio "github.com/hexiaodai/fence/internal/istio"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LogEntry struct {
	config.Server
	client.Client
	sidecar        *iistio.Sidecar
	namespaceCache *cache.Namespace
	ipServiceCache *cache.IpService
	resource       *Resource
	scheme         *runtime.Scheme
}

type HTTPAccessLogEntryWrapper struct {
	types.NamespacedName
	*data_accesslog.HTTPAccessLogEntry
	DestinationService DestinationService
}

type DestinationService int

const (
	Internal DestinationService = iota
	External
)

func NewLogEntry(client client.Client, scheme *runtime.Scheme, sidecar *iistio.Sidecar, namespaceCache *cache.Namespace, ipServiceCache *cache.IpService, resource *Resource, server config.Server) *LogEntry {
	server.Logger = server.Logger.WithName("StreamLogEntry").WithValues("controller", "LogEntry")
	return &LogEntry{
		Client:         client,
		scheme:         scheme,
		sidecar:        sidecar,
		namespaceCache: namespaceCache,
		ipServiceCache: ipServiceCache,
		resource:       resource,
		Server:         server,
	}
}

func (l *LogEntry) StreamLogEntry(logEntrys []*data_accesslog.HTTPAccessLogEntry) {
	for _, entry := range logEntrys {
		l.Logger.Sugar().Debugw("StreamLogEntry", "HTTPAccessLogEntry", entry)
		nn, err := l.getNamespacedName(entry)
		if err != nil {
			sourceIp, _ := l.ipServiceCache.FetchSourceIp(entry)
			l.Logger.Error(err, "failed to get sidecar namespaceName", "source ip", sourceIp)
			continue
		}

		log := l.Logger.WithValues("namespace", nn.Namespace, "service", nn.Name)

		if isSystemNamespace(l.FenceNamespace, l.IstioNamespace, nn.Namespace) {
			log.Sugar().Debugw("skip system namespace", "namespaceName", nn)
			continue
		}

		entryWrapper := &HTTPAccessLogEntryWrapper{
			DestinationService: l.destinationService(entry),
			NamespacedName:     nn,
			HTTPAccessLogEntry: entry,
		}

		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return l.resource.Refresh(context.Background(), entryWrapper)
		})
		if retryErr != nil {
			l.Logger.Error(retryErr, "failed to update sidecar, exceeded the maximum number of conflict retries", "namespaceName", nn)
			continue
		}
	}
}

func (l *LogEntry) getNamespacedName(entry *data_accesslog.HTTPAccessLogEntry) (out types.NamespacedName, err error) {
	sourceIp, err := l.ipServiceCache.FetchSourceIp(entry)
	if err != nil {
		return
	}
	sourceSvc, err := l.ipServiceCache.FetchSourceSvc(sourceIp)
	if err != nil {
		err = fmt.Errorf("failed to get source service. source ip is %v", sourceIp)
		return
	}
	return types.NamespacedName{Namespace: sourceSvc.Namespace, Name: sourceSvc.Name}, nil
}

func (l *LogEntry) destinationService(entry *data_accesslog.HTTPAccessLogEntry) DestinationService {
	dest := strings.Split(entry.Request.Authority, ":")[0]
	if dest == "" || net.ParseIP(dest) != nil {
		return External
	}

	destParts := strings.Split(dest, ".")
	if len(destParts) == 0 {
		return External
	}
	destSvc := types.NamespacedName{Name: destParts[0]}
	if len(destParts) == 1 {
		sourceIp, err := l.ipServiceCache.FetchSourceIp(entry)
		if err != nil {
			return External
		}
		sourceSvc, err := l.ipServiceCache.FetchSourceSvc(sourceIp)
		if err != nil {
			return External
		}
		destSvc.Namespace = sourceSvc.Namespace
	} else {
		destSvc.Namespace = destParts[1]
	}

	if _, ok := l.ipServiceCache.SvcToIps.Ips[destSvc]; ok {
		return Internal
	}
	return External
}
