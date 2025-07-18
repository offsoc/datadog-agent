// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build jmx

package jmxfetch

import (
	"time"

	"github.com/DataDog/datadog-agent/comp/agent/jmxlogger"
	"github.com/DataDog/datadog-agent/comp/core/autodiscovery/integration"
	ipc "github.com/DataDog/datadog-agent/comp/core/ipc/def"
	dogstatsdServer "github.com/DataDog/datadog-agent/comp/dogstatsd/server"
	pkgconfigsetup "github.com/DataDog/datadog-agent/pkg/config/setup"
	jmxStatus "github.com/DataDog/datadog-agent/pkg/status/jmx"
)

type runner struct {
	jmxfetch *JMXFetch
	started  bool
}

func (r *runner) initRunner(server dogstatsdServer.Component, logger jmxlogger.Component, ipc ipc.Component) {
	r.jmxfetch = NewJMXFetch(logger, ipc)
	r.jmxfetch.LogLevel = pkgconfigsetup.Datadog().GetString("log_level")
	r.jmxfetch.DSD = server
}

func (r *runner) startRunner() error {

	lifecycleMgmt := true
	err := r.jmxfetch.Start(lifecycleMgmt)
	if err != nil {
		s := jmxStatus.StartupError{LastError: err.Error(), Timestamp: time.Now().Unix()}
		jmxStatus.SetStartupError(s)
		return err
	}
	r.started = true
	return nil
}

func (r *runner) configureRunner(instance, initConfig integration.Data) error {
	if err := r.jmxfetch.ConfigureFromInstance(instance); err != nil {
		return err
	}
	return r.jmxfetch.ConfigureFromInitConfig(initConfig)
}

func (r *runner) stopRunner() error {
	if r.jmxfetch != nil && r.started {
		return r.jmxfetch.Stop()
	}
	return nil
}
