// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2024-present Datadog, Inc.

package modules

import (
	"github.com/DataDog/datadog-agent/cmd/system-probe/api/module"
	"github.com/DataDog/datadog-agent/cmd/system-probe/config"
	discoverymodule "github.com/DataDog/datadog-agent/pkg/collector/corechecks/servicediscovery/module"
)

// DiscoveryModule is the discovery module factory.
var DiscoveryModule = module.Factory{
	Name:             config.DiscoveryModule,
	ConfigNamespaces: []string{"discovery"},
	Fn:               discoverymodule.NewDiscoveryModule,
	NeedsEBPF: func() bool {
		return false
	},
	OptionalEBPF: true,
}
