package controller

import (
	"github.com/hexiaodai/fence/internal/cache"
	iconfig "github.com/hexiaodai/fence/internal/config"
	corev1 "k8s.io/api/core/v1"
)

type VarNamespace interface {
	*corev1.Namespace | *cache.Namespace
}

func fenceIsEnabled[T VarNamespace](namespace T, autoFence bool, pod *corev1.Pod) bool {
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
	return autoFence || nsEnabled || svcEnabled
}

func namespaceIsDisable(ns *corev1.Namespace) bool {
	return ns.Labels[iconfig.SidecarFenceLabel] == iconfig.SidecarFenceValueDisable
}

func isInjectSidecar(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == "istio-proxy" {
			return true
		}
	}
	return false
}

func isSystemNamespace(namespace, istioNamespace, targetNs string) bool {
	include := map[string]struct{}{namespace: {}, istioNamespace: {}, "kube-system": {}}
	_, ok := include[targetNs]
	return ok
}
