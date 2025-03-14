// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package apiimpl

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gorilla "github.com/gorilla/mux"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/DataDog/datadog-agent/comp/api/api/apiimpl/internal/agent"
	"github.com/DataDog/datadog-agent/comp/api/api/apiimpl/internal/check"
	"github.com/DataDog/datadog-agent/comp/api/api/apiimpl/observability"
	"github.com/DataDog/datadog-agent/comp/core/config"
	taggerserver "github.com/DataDog/datadog-agent/comp/core/tagger/server"
	workloadmetaServer "github.com/DataDog/datadog-agent/comp/core/workloadmeta/server"
	pkgconfigsetup "github.com/DataDog/datadog-agent/pkg/config/setup"
	pb "github.com/DataDog/datadog-agent/pkg/proto/pbgo/core"
	grpcutil "github.com/DataDog/datadog-agent/pkg/util/grpc"
)

const cmdServerName string = "CMD API Server"
const cmdServerShortName string = "CMD"

func (server *apiServer) startCMDServer(
	cmdAddr string,
	tmf observability.TelemetryMiddlewareFactory,
	cfg config.Component,
) (err error) {
	// get the transport we're going to use under HTTP
	server.cmdListener, err = getListener(cmdAddr)
	if err != nil {
		// we use the listener to handle commands for the Agent, there's
		// no way we can recover from this error
		return fmt.Errorf("unable to listen to the given address: %v", err)
	}

	// gRPC server
	authInterceptor := grpcutil.AuthInterceptor(parseToken)

	maxMessageSize := cfg.GetInt("cluster_agent.cluster_tagger.grpc_max_message_size")

	opts := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(server.authToken.GetTLSServerConfig())),
		grpc.StreamInterceptor(grpc_auth.StreamServerInterceptor(authInterceptor)),
		grpc.UnaryInterceptor(grpc_auth.UnaryServerInterceptor(authInterceptor)),
		grpc.MaxRecvMsgSize(maxMessageSize),
		grpc.MaxSendMsgSize(maxMessageSize),
	}

	// event size should be small enough to fit within the grpc max message size
	maxEventSize := maxMessageSize / 2
	s := grpc.NewServer(opts...)
	pb.RegisterAgentServer(s, &grpcServer{})
	pb.RegisterAgentSecureServer(s, &serverSecure{
		configService:    server.rcService,
		configServiceMRF: server.rcServiceMRF,
		taggerServer:     taggerserver.NewServer(server.taggerComp, maxEventSize, cfg.GetInt("remote_tagger.max_concurrent_sync")),
		taggerComp:       server.taggerComp,
		// TODO(components): decide if workloadmetaServer should be componentized itself
		workloadmetaServer:  workloadmetaServer.NewServer(server.wmeta),
		dogstatsdServer:     server.dogstatsdServer,
		capture:             server.capture,
		pidMap:              server.pidMap,
		remoteAgentRegistry: server.remoteAgentRegistry,
		autodiscovery:       server.autoConfig,
		configComp:          cfg,
	})

	dopts := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(server.authToken.GetTLSClientConfig()))}

	// starting grpc gateway
	ctx := context.Background()
	gwmux := runtime.NewServeMux()
	err = pb.RegisterAgentHandlerFromEndpoint(
		ctx, gwmux, cmdAddr, dopts)
	if err != nil {
		return fmt.Errorf("error registering agent handler from endpoint %s: %v", cmdAddr, err)
	}

	err = pb.RegisterAgentSecureHandlerFromEndpoint(
		ctx, gwmux, cmdAddr, dopts)
	if err != nil {
		return fmt.Errorf("error registering agent secure handler from endpoint %s: %v", cmdAddr, err)
	}

	// Setup multiplexer
	// create the REST HTTP router
	agentMux := gorilla.NewRouter()
	checkMux := gorilla.NewRouter()

	// Validate token for every request
	agentMux.Use(validateToken)
	checkMux.Use(validateToken)

	cmdMux := http.NewServeMux()
	cmdMux.Handle(
		"/agent/",
		http.StripPrefix("/agent",
			agent.SetupHandlers(
				agentMux,
				server.wmeta,
				server.senderManager,
				server.secretResolver,
				server.collector,
				server.autoConfig,
				server.endpointProviders,
				server.taggerComp,
			)))
	cmdMux.Handle("/check/", http.StripPrefix("/check", check.SetupHandlers(checkMux)))
	cmdMux.Handle("/", gwmux)

	// Add some observability in the API server
	cmdMuxHandler := tmf.Middleware(cmdServerShortName)(cmdMux)
	cmdMuxHandler = observability.LogResponseHandler(cmdServerName)(cmdMuxHandler)

	srv := grpcutil.NewMuxedGRPCServer(
		cmdAddr,
		server.authToken.GetTLSServerConfig(),
		s,
		grpcutil.TimeoutHandlerFunc(cmdMuxHandler, time.Duration(pkgconfigsetup.Datadog().GetInt64("server_timeout"))*time.Second),
	)

	startServer(server.cmdListener, srv, cmdServerName)

	return nil
}
