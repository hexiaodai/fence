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
	"github.com/hexiaodai/fence/internal/config"
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
	// map[string]types.NamespacedName
	IpToService sync.Map
	// map[types.NamespacedName][]string
	ServiceToIps sync.Map
	config.Server
}

func NewIpService(server config.Server) *IpService {
	server.Logger = server.Logger.WithName("IpService").WithValues("cache", "IpService")
	return &IpService{
		Server:       server,
		IpToService:  sync.Map{},
		ServiceToIps: sync.Map{},
	}
}

func (i *IpService) Start(ctx context.Context) error {
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

	i.Logger.Info("started")
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
	svc := types.NamespacedName{Namespace: ep.GetNamespace(), Name: ep.GetName()}
	var addresses []string
	for _, subset := range ep.Subsets {
		for _, address := range subset.Addresses {
			addresses = append(addresses, address.IP)
			i.IpToService.Store(address.IP, svc)
		}
	}
	i.ServiceToIps.Store(svc, addresses)
}

func (i *IpService) deleteIpFromEp(ep *corev1.Endpoints) {
	svc := types.NamespacedName{Namespace: ep.GetNamespace(), Name: ep.GetName()}

	// delete svc in ServiceToIps
	value, ok := i.ServiceToIps.LoadAndDelete(svc)
	if !ok {
		return
	}
	ips := value.([]string)

	// delete ips related svc
	for _, ip := range ips {
		i.IpToService.Delete(ip)
	}
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

func (i *IpService) FetchSourceSvc(sourceIp string) (*types.NamespacedName, error) {
	value, ok := i.IpToService.Load(sourceIp)
	if !ok {
		return nil, fmt.Errorf("no source service, source ip is %v", sourceIp)
	}

	svc, ok := value.(types.NamespacedName)
	if !ok {
		return nil, fmt.Errorf("failed to get source service, source ip is %v", sourceIp)
	}
	return &svc, nil
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
	destSvc = dest
	// destParts: name.namespace.svc.cluster.local
	svc := types.NamespacedName{Namespace: destParts[1], Name: destParts[0]}
	if _, ok := i.ServiceToIps.Load(svc); ok {
		destSvc = fmt.Sprintf("%v.%v", dest, suffix)
	}
	return
}
