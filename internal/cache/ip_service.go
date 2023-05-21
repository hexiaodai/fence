package cache

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	envoy_config_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/log"
	"github.com/hexiaodai/fence/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type IpService struct {
	IpToSvc  *IpToSvc
	SvcToIps *SvcToIps
	log      logr.Logger
}

type IpToSvc struct {
	Svc map[string]Svc
	sync.RWMutex
}

type SvcToIps struct {
	Ips map[types.NamespacedName][]string
	sync.RWMutex
}

type Svc struct {
	Namespace string
	Name      string
}

func NewIpService() *IpService {
	return &IpService{
		IpToSvc:  &IpToSvc{Svc: make(map[string]Svc)},
		SvcToIps: &SvcToIps{Ips: make(map[types.NamespacedName][]string)},
	}
}

func (i *IpService) Start(ctx context.Context) error {
	logger, err := log.NewLogger()
	if err != nil {
		return err
	}

	i.log = logger.WithValues("cache", "IpService")

	config, err := options.DefaultConfigFlags.ToRawKubeConfigLoader().ClientConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Endpoints("").List(ctx, metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Endpoints("").Watch(ctx, metav1.ListOptions{})
		},
	}

	_, controller := cache.NewInformer(lw, &corev1.Endpoints{}, 60*time.Second, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { i.handleEpAdd(ctx, obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { i.handleEpUpdate(ctx, oldObj, newObj) },
		DeleteFunc: func(obj interface{}) { i.handleEpDelete(ctx, obj) },
	})

	go controller.Run(ctx.Done())

	i.log.Info("started")
	return nil
}

func (i *IpService) handleEpAdd(ctx context.Context, obj interface{}) {
	ep, ok := obj.(*corev1.Endpoints)
	if !ok {
		return
	}
	i.addIpWithEp(ep)
}

func (i *IpService) handleEpUpdate(ctx context.Context, old, obj interface{}) {
	ep, ok := obj.(*corev1.Endpoints)
	if !ok {
		return
	}
	oldEp, ok := old.(*corev1.Endpoints)
	if !ok {
		return
	}

	if reflect.DeepEqual(oldEp.Subsets, ep.Subsets) {
		return
	}

	i.deleteIpFromEp(oldEp)
	i.addIpWithEp(ep)
}

func (i *IpService) handleEpDelete(ctx context.Context, obj interface{}) {
	ep, ok := obj.(*corev1.Endpoints)
	if !ok {
		return
	}
	i.deleteIpFromEp(ep)
}

func (i *IpService) addIpWithEp(ep *corev1.Endpoints) {
	svc := Svc{Namespace: ep.GetNamespace(), Name: ep.GetName()}
	ipToSvcCache := i.IpToSvc
	svcToIpsCache := i.SvcToIps

	var addresses []string
	ipToSvcCache.Lock()
	for _, subset := range ep.Subsets {
		for _, address := range subset.Addresses {
			addresses = append(addresses, address.IP)
			ipToSvcCache.Svc[address.IP] = svc
		}
	}
	ipToSvcCache.Unlock()

	svcToIpsCache.Lock()
	svcToIpsCache.Ips[types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}] = addresses
	svcToIpsCache.Unlock()
}

func (i *IpService) deleteIpFromEp(ep *corev1.Endpoints) {
	nn := types.NamespacedName{Namespace: ep.GetNamespace(), Name: ep.GetName()}
	ipToSvcCache := i.IpToSvc
	svcToIpsCache := i.SvcToIps

	// delete svc in svcToIpsCache
	svcToIpsCache.Lock()
	ips := svcToIpsCache.Ips[nn]
	delete(svcToIpsCache.Ips, nn)
	svcToIpsCache.Unlock()

	// delete ips related svc
	ipToSvcCache.Lock()
	for _, ip := range ips {
		delete(ipToSvcCache.Svc, ip)
	}
	ipToSvcCache.Unlock()
}

func (i *IpService) FetchSourceIp(entry *data_accesslog.HTTPAccessLogEntry) (sourceIp string, err error) {
	downstreamSock := entry.CommonProperties.DownstreamRemoteAddress.Address.(*envoy_config_core.Address_SocketAddress)
	if net.ParseIP(downstreamSock.SocketAddress.Address) == nil {
		err = fmt.Errorf("source ip does not exist")
		return
	}
	sourceIp = downstreamSock.SocketAddress.Address
	return
}

func (i *IpService) FetchSourceSvc(sourceIp string) (out *Svc, err error) {
	i.IpToSvc.RLock()
	defer i.IpToSvc.RUnlock()

	if svc, ok := i.IpToSvc.Svc[sourceIp]; ok {
		out = &svc
		return
	}
	err = fmt.Errorf("no source service, source ip is %v", sourceIp)
	return
}

func (i *IpService) FetchDestinationSvc(entry *data_accesslog.HTTPAccessLogEntry) (destSvc string, err error) {
	upstreamCluster := entry.CommonProperties.UpstreamCluster
	parts := strings.Split(upstreamCluster, "|")
	if len(parts) != 4 {
		err = fmt.Errorf("upstreamCluster is wrong: parts number is not 4, upstreamCluster is %v", upstreamCluster)
		return
	}
	// only handle inbound access log
	if parts[0] != "inbound" {
		err = fmt.Errorf("this log is not inbound")
		return
	}
	// get destination service info from request.authority
	auth := entry.Request.Authority
	dest := strings.Split(auth, ":")[0]

	// dest is ip address, skip
	if net.ParseIP(dest) != nil {
		err = fmt.Errorf("destination is ip address")
		return
	}

	// both short name and k8s fqdn will be added as following

	destParts := strings.Split(dest, ".")

	sourceIp, err := i.FetchSourceIp(entry)
	if err != nil {
		err = fmt.Errorf("failed to fetch source ip")
		return
	}
	sourceSvc, err := i.FetchSourceSvc(sourceIp)
	if err != nil {
		return
	}

	destSvc = dest
	switch len(destParts) {
	case 1:
		destSvc = fmt.Sprintf("%v.%v.svc.cluster.local", dest, sourceSvc.Namespace)
	case 2:
		destSvc = i.completeDestSvcName(destParts, dest, "svc.cluster.local")
	case 3:
		if destParts[2] == "svc" {
			destSvc = i.completeDestSvcName(destParts, dest, "cluster.local")
		}
	}

	return
}

func (i *IpService) completeDestSvcName(destParts []string, dest, suffix string) (destSvc string) {
	i.SvcToIps.RLock()
	defer i.SvcToIps.RUnlock()

	destSvc = dest
	// destParts: name.namespace.svc.cluster.local
	svcnn := types.NamespacedName{Namespace: destParts[1], Name: destParts[0]}
	if _, ok := i.SvcToIps.Ips[svcnn]; ok {
		// dest is abbreviation of service, add suffix
		destSvc = fmt.Sprintf("%v.%v", dest, suffix)
	}
	return
}
