package metric

import (
	"fmt"
	"net"

	data_accesslog "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	service_accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v3"
	"github.com/hexiaodai/fence/internal/config"
	"google.golang.org/grpc"
)

type HttpLogEntry interface {
	StreamLogEntry([]*data_accesslog.HTTPAccessLogEntry)
}

type AccessLogSource struct {
	servePort    string
	httpLogEntry HttpLogEntry
	config.Server
}

func (a *AccessLogSource) Name() string {
	return "AccessLogSource"
}

func NewAccessLogSource(servePort string, server config.Server) (*AccessLogSource, error) {
	source := &AccessLogSource{
		Server:    server,
		servePort: servePort,
	}
	source.Logger = source.Logger.WithName(source.Name()).WithValues("metric", source.Name())
	return source, nil
}

func (s *AccessLogSource) RegisterHttpLogEntry(h HttpLogEntry) {
	s.httpLogEntry = h
}

// StreamAccessLogs accept access log from fence xds egress gateway
func (s *AccessLogSource) StreamAccessLogs(logServer service_accesslog.AccessLogService_StreamAccessLogsServer) error {
	for {
		message, err := logServer.Recv()
		if err != nil {
			return err
		}

		httpLogEntries := message.GetHttpLogs()
		if httpLogEntries != nil && s.httpLogEntry != nil {
			s.httpLogEntry.StreamLogEntry(httpLogEntries.LogEntry)
		}
	}
}

// Start grpc server
func (s *AccessLogSource) Start() error {
	listen, err := net.Listen("tcp", fmt.Sprintf(":%v", s.servePort))
	if err != nil {
		return err
	}

	server := grpc.NewServer()
	service_accesslog.RegisterAccessLogServiceServer(server, s)

	go func() {
		if err = server.Serve(listen); err != nil {
			panic(err)
		}
	}()

	s.Logger.Info("accessLogSource server is starting to listen", "addr", s.servePort)
	return nil
}
