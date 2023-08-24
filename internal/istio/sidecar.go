package istio

import (
	"errors"
	"fmt"

	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	icache "github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	istio "istio.io/api/networking/v1alpha3"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ErrNoLabelSelector = errors.New("no label selector")
)

type Sidecar struct {
	ipServiceCache *icache.IpService
	config.Server
}

func NewSidecar(ipServiceCache *icache.IpService, server config.Server) *Sidecar {
	return &Sidecar{ipServiceCache: ipServiceCache, Server: server}
}

func (s *Sidecar) Generate(svc *corev1.Service) (*networkingv1alpha3.Sidecar, error) {
	if len(svc.Spec.Selector) == 0 {
		return nil, ErrNoLabelSelector
	}
	sidecar := &networkingv1alpha3.Sidecar{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		},
		Spec: istio.Sidecar{
			WorkloadSelector: &istio.WorkloadSelector{
				Labels: svc.Spec.Selector,
			},
			Egress: s.generateDefaultEgress(),
		},
	}
	return sidecar, nil
}

func (s *Sidecar) generateDefaultEgress() []*istio.IstioEgressListener {
	return []*istio.IstioEgressListener{
		{
			Hosts: []string{
				fmt.Sprintf("%s/*", s.IstioNamespace),
				fmt.Sprintf("%s/*", s.Namespace),
			},
		},
	}
}

func (s *Sidecar) AddDestinationSvcToEgress(sidecar *networkingv1alpha3.Sidecar, entry *data_accesslog.HTTPAccessLogEntry) error {
	destSvc, err := s.ipServiceCache.FetchDestinationSvc(entry)
	if err != nil {
		return fmt.Errorf("get destination domain error, error: %v", err)
	}
	hostIndexer := map[string]struct{}{}
	egress := sidecar.Spec.Egress
	if len(egress) == 0 {
		egress = s.generateDefaultEgress()
	}
	for _, host := range egress[0].Hosts {
		hostIndexer[host] = struct{}{}
	}
	hostIndexer[fmt.Sprintf("*/%v", destSvc)] = struct{}{}
	hosts := []string{}
	for host := range hostIndexer {
		hosts = append(hosts, host)
	}
	egress[0].Hosts = hosts
	sidecar.Spec.Egress = egress
	return nil
}
