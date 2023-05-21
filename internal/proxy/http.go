package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/log"
	"k8s.io/apimachinery/pkg/types"
)

const (
	HeaderSourceNs = "Fence-Source-Ns"
	HeaderOrigDest = "Fence-Orig-Dest"
)

func NewHttpProxy(wormholePort string, serviceCache *cache.Service) (*HttpProxy, error) {
	logger, err := log.NewLogger()
	if err != nil {
		return nil, err
	}
	return &HttpProxy{
		wormholePort: wormholePort,
		serviceCache: serviceCache,
		log:          logger.WithValues("proxy", "HttpProxy"),
	}, nil
}

type HttpProxy struct {
	wormholePort string
	serviceCache *cache.Service
	log          logr.Logger
}

func (h *HttpProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.log.Info("request", "proto", req.Proto, "method", req.Method, "host", req.Host)

	var (
		reqCtx               = req.Context()
		reqHost              = req.Host
		origDest, origDestIp string
		origDestPort         = h.wormholePort
	)

	if values := req.Header[HeaderSourceNs]; len(values) > 0 && values[0] != "" {
		req.Header.Del(HeaderSourceNs)

		// we do not sure if reqHost is k8s short name or no ns service
		// so k8s svc will be extended/searched first
		// otherwise original reqHost is used

		if !strings.Contains(reqHost, ".") {
			// short name
			var (
				ns      = values[0]
				svcName = reqHost
				port    string
			)

			// if host has port info, extract it
			idx := strings.LastIndex(reqHost, ":")
			if idx >= 0 {
				svcName = reqHost[:idx]
				port = reqHost[idx+1:]
			}

			nn := types.NamespacedName{
				Namespace: ns,
				Name:      svcName,
			}

			// it means svc controller is disabled when SvcCache is nil,
			// so, all short domain should add ns info

			if h.serviceCache == nil || h.serviceCache.ExistNcName(nn) {
				if idx >= 0 {
					// add port info
					reqHost = fmt.Sprintf("%s.%s:%s", nn.Name, nn.Namespace, port)
				} else {
					reqHost = fmt.Sprintf("%s.%s", nn.Name, nn.Namespace)
				}
			}
		}
	}

	if values := req.Header[HeaderOrigDest]; len(values) > 0 {
		origDest = values[0]
		req.Header.Del(HeaderOrigDest)

		if idx := strings.LastIndex(origDest, ":"); idx >= 0 {
			origDestIp = origDest[:idx]
			if origDest[idx+1:] == "" {
				http.Error(w, fmt.Sprintf("invalid header %s value: %s", HeaderOrigDest, origDest), http.StatusBadRequest)
				return
			}
			origDestPort = origDest[idx+1:]
		} else {
			origDestIp = origDest
		}
	}

	if origDest == "" {
		if idx := strings.LastIndex(reqHost, ":"); idx >= 0 {
			origDestIp = reqHost[:idx]
		} else {
			origDestIp = reqHost
		}
	}

	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	req.URL.Host = reqHost
	req.Host = reqHost
	req.RequestURI = ""
	newCtx, cancel := context.WithCancel(reqCtx)
	defer cancel()
	req = req.WithContext(newCtx)

	dialer := &net.Dialer{
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			newAddr := fmt.Sprintf("%s:%s", origDestIp, origDestPort)
			return dialer.DialContext(ctx, network, newAddr)
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
	}

	resp, err := client.Do(req)
	if err != nil {
		select {
		case <-reqCtx.Done():
		default:
			h.log.Info(err.Error())
			http.Error(w, "", http.StatusInternalServerError)
		}
		return
	}

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.log.Info(err.Error())
	}
}
