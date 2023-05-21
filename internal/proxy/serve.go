package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/log"
	"golang.org/x/sys/unix"
)

func NewServe(serviceCache *cache.Service, config config.FenceProxy) (*Serve, error) {
	logger, err := log.NewLogger()
	if err != nil {
		return nil, err
	}
	s := &Serve{
		serviceCache: serviceCache,
		servers:      make(map[string]*http.Server),
		config:       config,
	}
	s.log = logger.WithValues("proxy", s.Name())
	return s, nil
}

type Serve struct {
	serverMutex  sync.RWMutex
	servers      map[string]*http.Server
	serviceCache *cache.Service
	log          logr.Logger
	config       config.FenceProxy
}

func (s *Serve) Name() string {
	return "Serve"
}

func (s *Serve) ListenAndServe(wormholePorts ...string) {
	s.serverMutex.Lock()
	defer s.serverMutex.Unlock()

	s.log.Info("starting listen and serve with wormholePorts", "wormholePorts", wormholePorts)
	for _, whPort := range wormholePorts {
		if _, exist := s.servers[whPort]; !exist {
			if whPort == s.config.ProbePort {
				s.log.Info("probePort is conflict with wormholePort, skip port bind", "wormholePort", whPort)
				continue
			}
			handler, err := NewHttpProxy(whPort, s.serviceCache)
			if err != nil {
				s.log.Error(err, "skip port bind", "wormholePort", whPort)
				continue
			}
			srv := &http.Server{
				Addr:    fmt.Sprintf(":%v", whPort),
				Handler: handler,
			}
			s.servers[whPort] = srv
			go s.startServer(srv)
		}
	}
	s.log.Info("started")
}

func (s *Serve) startServer(srv *http.Server) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEADDR, 1)
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
		},
	}
	l, err := lc.Listen(context.Background(), "tcp", srv.Addr)
	if err != nil {
		s.log.Error(err, "proxy listen error")
		return
	}
	if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
		s.log.Error(err, "proxy serve error")
	}
}

func (s *Serve) ShutdownServer(wormholePort int32) error {
	srv := s.servers[strconv.Itoa(int(wormholePort))]
	if srv == nil {
		return nil
	}
	s.log.Info("stopting proxy", "addr", srv.Addr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
