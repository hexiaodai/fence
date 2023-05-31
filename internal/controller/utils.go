package controller

import (
	"github.com/hexiaodai/fence/internal/cache"
	iconfig "github.com/hexiaodai/fence/internal/config"
	corev1 "k8s.io/api/core/v1"
)

type VarNamespace interface {
	*corev1.Namespace | *cache.Namespace
}

func fenceIsEnabled[T VarNamespace](namespace T, config iconfig.Fence, pod *corev1.Pod) bool {
	var nsEnabled bool
	switch any(namespace).(type) {
	case *corev1.Namespace:
		// namespace
		ns, ok := any(namespace).(*corev1.Namespace)
		if !ok {
			return false
		}
		nsEnabled = ns.Labels[iconfig.SidecarFenceLabel] == iconfig.SidecarFenceValueEnabled
		if ns.Labels[iconfig.SidecarFenceLabel] == iconfig.SidecarFenceValueDisable {
			return false
		}
	case *cache.Namespace:
		// namespace
		nsc, ok := any(namespace).(*cache.Namespace)
		if !ok {
			return false
		}
		nsEnabled = nsc.IsEnabled(pod.Namespace)
		if nsc.IsDisable(pod.Namespace) {
			return false
		}
	default:
		return false
	}
	// pod
	if pod.Labels[iconfig.SidecarFenceLabel] == iconfig.SidecarFenceValueDisable {
		return false
	}

	svcEnabled := pod.Labels[iconfig.SidecarFenceLabel] == iconfig.SidecarFenceValueEnabled
	return config.AutoFence || nsEnabled || svcEnabled
}

func isSystemNamespace(config iconfig.Fence, targetNs string) bool {
	include := map[string]struct{}{config.IstioNamespace: {}, config.FenceNamespace: {}, "kube-system": {}}
	_, ok := include[targetNs]
	return ok
}
