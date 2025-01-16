// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build windows

// Package main implements main
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"go.uber.org/fx"

	"github.com/DataDog/datadog-agent/cmd/internal/runcmd"
	"github.com/DataDog/datadog-agent/cmd/security-agent/command"
	saconfig "github.com/DataDog/datadog-agent/cmd/security-agent/config"
	"github.com/DataDog/datadog-agent/cmd/security-agent/subcommands"
	"github.com/DataDog/datadog-agent/cmd/security-agent/subcommands/runtime"
	"github.com/DataDog/datadog-agent/cmd/security-agent/subcommands/start"
	"github.com/DataDog/datadog-agent/comp/agent/autoexit"
	"github.com/DataDog/datadog-agent/comp/agent/autoexit/autoexitimpl"
	"github.com/DataDog/datadog-agent/comp/api/authtoken"
	"github.com/DataDog/datadog-agent/comp/api/authtoken/fetchonlyimpl"
	"github.com/DataDog/datadog-agent/comp/core"
	"github.com/DataDog/datadog-agent/comp/core/config"
	"github.com/DataDog/datadog-agent/comp/core/configsync/configsyncimpl"
	log "github.com/DataDog/datadog-agent/comp/core/log/def"
	"github.com/DataDog/datadog-agent/comp/core/secrets"
	"github.com/DataDog/datadog-agent/comp/core/settings"
	"github.com/DataDog/datadog-agent/comp/core/settings/settingsimpl"
	"github.com/DataDog/datadog-agent/comp/core/status"
	"github.com/DataDog/datadog-agent/comp/core/status/statusimpl"
	"github.com/DataDog/datadog-agent/comp/core/sysprobeconfig"
	"github.com/DataDog/datadog-agent/comp/core/sysprobeconfig/sysprobeconfigimpl"
	"github.com/DataDog/datadog-agent/comp/core/telemetry"
	wmcatalog "github.com/DataDog/datadog-agent/comp/core/workloadmeta/collectors/catalog"
	workloadmeta "github.com/DataDog/datadog-agent/comp/core/workloadmeta/def"
	workloadmetafx "github.com/DataDog/datadog-agent/comp/core/workloadmeta/fx"
	"github.com/DataDog/datadog-agent/comp/dogstatsd"
	"github.com/DataDog/datadog-agent/comp/dogstatsd/statsd"
	"github.com/DataDog/datadog-agent/comp/metadata/host/hostimpl"
	logscompression "github.com/DataDog/datadog-agent/comp/serializer/logscompression/def"
	logscompressionfx "github.com/DataDog/datadog-agent/comp/serializer/logscompression/fx"
	commonsettings "github.com/DataDog/datadog-agent/pkg/config/settings"

	"github.com/DataDog/datadog-agent/pkg/collector/python"
	"github.com/DataDog/datadog-agent/pkg/config/setup"
	"github.com/DataDog/datadog-agent/pkg/security/agent"
	"github.com/DataDog/datadog-agent/pkg/security/utils/hostnameutils"

	"github.com/DataDog/datadog-agent/pkg/util/defaultpaths"
	"github.com/DataDog/datadog-agent/pkg/util/fxutil"
	"github.com/DataDog/datadog-agent/pkg/util/startstop"
	"github.com/DataDog/datadog-agent/pkg/util/winutil/servicemain"
)

type service struct {
	servicemain.DefaultSettings
}

var (
	defaultSecurityAgentConfigFilePaths = []string{
		path.Join(defaultpaths.ConfPath, "datadog.yaml"),
		path.Join(defaultpaths.ConfPath, "security-agent.yaml"),
	}
	defaultSysProbeConfPath = path.Join(defaultpaths.ConfPath, "system-probe.yaml")
)

// Name returns the service name
func (s *service) Name() string {
	return saconfig.ServiceName
}

// Init does nothing for now.
func (s *service) Init() error {
	return nil
}

type cliParams struct {
	*command.GlobalParams
}

// Run actually runs the service; blocks until service exits.
func (s *service) Run(svcctx context.Context) error {

	params := &cliParams{}
	err := fxutil.OneShot(
		func(log log.Component, config config.Component, _ secrets.Component, _ statsd.Component, _ sysprobeconfig.Component,
			telemetry telemetry.Component, _ workloadmeta.Component, _ *cliParams, statusComponent status.Component, _ autoexit.Component,
			settings settings.Component, wmeta workloadmeta.Component, at authtoken.Component) error {
			defer start.StopAgent(log)

			err := start.RunAgent(log, config, telemetry, statusComponent, settings, wmeta, at)
			if err != nil {
				if errors.Is(err, start.ErrAllComponentsDisabled) {
					// If all components are disabled, we should exit cleanly
					return fmt.Errorf("%w: %w", servicemain.ErrCleanStopAfterInit, err)
				}
				return err
			}

			// Wait for stop signal
			<-svcctx.Done()
			log.Info("Received stop from service manager, shutting down...")

			return nil
		},
		fx.Supply(params),
		fx.Supply(core.BundleParams{
			ConfigParams:         config.NewSecurityAgentParams(defaultSecurityAgentConfigFilePaths),
			SecretParams:         secrets.NewEnabledParams(),
			SysprobeConfigParams: sysprobeconfigimpl.NewParams(sysprobeconfigimpl.WithSysProbeConfFilePath(defaultSysProbeConfPath)),
			LogParams:            log.ForDaemon(command.LoggerName, "security_agent.log_file", setup.DefaultSecurityAgentLogFile),
		}),
		core.Bundle(),
		dogstatsd.ClientBundle,

		// workloadmeta setup
		wmcatalog.GetCatalog(),
		workloadmetafx.Module(workloadmeta.Params{
			AgentType: workloadmeta.Remote,
		}),
		fx.Provide(func(log log.Component, config config.Component, statsd statsd.Component, wmeta workloadmeta.Component, compression logscompression.Component) (status.InformationProvider, *agent.RuntimeSecurityAgent, error) {
			stopper := startstop.NewSerialStopper()

			statsdClient, err := statsd.CreateForHostPort(setup.GetBindHost(config), config.GetInt("dogstatsd_port"))

			if err != nil {
				return status.NewInformationProvider(nil), nil, err
			}

			hostnameDetected, err := hostnameutils.GetHostnameWithContextAndFallback(context.TODO())
			if err != nil {
				return status.NewInformationProvider(nil), nil, err
			}

			runtimeAgent, err := runtime.StartRuntimeSecurity(log, config, hostnameDetected, stopper, statsdClient, wmeta, compression)
			if err != nil {
				return status.NewInformationProvider(nil), nil, err
			}

			if runtimeAgent == nil {
				return status.NewInformationProvider(nil), nil, nil
			}

			// TODO - components: Do not remove runtimeAgent ref until "github.com/DataDog/datadog-agent/pkg/security/agent" is a component so they're not GCed
			return status.NewInformationProvider(runtimeAgent.StatusProvider()), runtimeAgent, nil
		}),
		fx.Supply(
			status.Params{
				PythonVersionGetFunc: python.GetPythonVersion,
			},
		),
		fx.Provide(func(config config.Component) status.HeaderInformationProvider {
			return status.NewHeaderInformationProvider(hostimpl.StatusProvider{
				Config: config,
			})
		}),

		statusimpl.Module(),

		fetchonlyimpl.Module(),
		configsyncimpl.Module(configsyncimpl.NewDefaultParams()),
		autoexitimpl.Module(),
		fx.Provide(func(c config.Component) settings.Params {
			return settings.Params{
				Settings: map[string]settings.RuntimeSetting{
					"log_level": commonsettings.NewLogLevelRuntimeSetting(),
				},
				Config: c,
			}
		}),
		settingsimpl.Module(),
		logscompressionfx.Module(),
	)

	return err
}

func main() {
	// if command line arguments are supplied, even in a non-interactive session,
	// then just execute that.  Used when the service is executing the executable,
	// for instance to trigger a restart.
	if len(os.Args) == 1 {
		if servicemain.RunningAsWindowsService() {
			servicemain.Run(&service{})
			return
		}
	}

	rootCmd := command.MakeCommand(subcommands.SecurityAgentSubcommands())
	os.Exit(runcmd.Run(rootCmd))
}
