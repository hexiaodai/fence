package controller

import (
	"context"
	goerrors "errors"
	"fmt"
	"net"
	"strings"

	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	iistio "github.com/hexiaodai/fence/internal/istio"
	"github.com/hexiaodai/fence/internal/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LogEntry struct {
	client.Client
	sidecar        *iistio.Sidecar
	namespaceCache *cache.Namespace
	ipServiceCache *cache.IpService
	resource       *Resource
	config         config.Fence
	log            logr.Logger
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

func NewLogEntry(client client.Client, sidecar *iistio.Sidecar, namespaceCache *cache.Namespace, ipServiceCache *cache.IpService, resource *Resource, config config.Fence) *LogEntry {
	logger, err := log.NewLogger()
	if err != nil {
		panic(err)
	}
	logger = logger.WithValues("LogEntry", "StreamLogEntry")

	return &LogEntry{Client: client, sidecar: sidecar, namespaceCache: namespaceCache, ipServiceCache: ipServiceCache, resource: resource, config: config, log: logger}
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

		pod, err := l.fetchPod(nn)
		if err != nil {
			if errors.IsNotFound(err) || goerrors.Is(err, errFetchPodNotFound) {
				continue
			}
			l.log.Error(err, "failed to get pod", "namespaceName", nn)
			continue
		}

		if !fenceIsEnabled(l.namespaceCache, l.config, pod) {
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
			l.log.Error(retryErr, "failed to update sidecar, exceeded the maximum number of conflict retries", "namespaceName", nn)
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
		err = fmt.Errorf("failed to get source service, source ip is %v", sourceIp)
		return
	}
	return types.NamespacedName{Namespace: sourceSvc.Namespace, Name: sourceSvc.Name}, nil
}

func (l *LogEntry) fetchPod(nn types.NamespacedName) (*corev1.Pod, error) {
	deploy := &appsv1.Deployment{}
	if err := l.Client.Get(context.Background(), nn, deploy); err != nil {
		return nil, fmt.Errorf("failed to get deployment: %v", err)
	}

	list := &corev1.PodList{}
	if err := l.Client.List(context.Background(), list, &client.ListOptions{
		LabelSelector: labels.Set(deploy.Labels).AsSelector(),
	}); err != nil {
		return nil, fmt.Errorf("failed to list pod: %v", err)
	}
	if len(list.Items) == 0 {
		return nil, errFetchPodNotFound
	}

	return &list.Items[0], nil
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
