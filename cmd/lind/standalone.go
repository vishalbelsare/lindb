// Licensed to LinDB under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. LinDB licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package main

import (
	"fmt"

	"github.com/lindb/common/pkg/fileutil"
	"github.com/lindb/common/pkg/ltoml"
	"github.com/spf13/cobra"

	"github.com/lindb/lindb/app/standalone"
	"github.com/lindb/lindb/config"
	"github.com/lindb/lindb/pkg/logger"
)

const (
	standaloneCfgName     = "standalone.toml"
	standaloneLogFileName = "lind-standalone.log"
	// DefaultStandaloneCfgFile defines default config file path for standalone mode
	defaultStandaloneCfgFile = currentDir + standaloneCfgName
)

// newStandaloneCmd returns a new standalone-cmd
func newStandaloneCmd() *cobra.Command {
	standaloneCmd := &cobra.Command{
		Use:   "standalone",
		Short: "Run as a standalone node with embed broker, storage, etcd)",
	}

	standaloneCmd.AddCommand(
		runStandaloneCmd,
		initializeStandaloneConfigCmd,
	)

	runStandaloneCmd.PersistentFlags().StringVar(&cfg, "config", "",
		fmt.Sprintf("config file path for standalone mode, default is %s", defaultStandaloneCfgFile))
	runStandaloneCmd.PersistentFlags().BoolVar(&doc, "doc", false,
		"enable swagger api doc")
	runStandaloneCmd.PersistentFlags().BoolVar(&pprof, "pprof", false,
		"profiling Go programs with pprof")
	runStandaloneCmd.PersistentFlags().BoolVar(&embedEtcd, "embed-etcd", true,
		"enable embed etcd server")

	return standaloneCmd
}

var runStandaloneCmd = &cobra.Command{
	Use:   "run",
	Short: "run as standalone mode",
	RunE:  serveStandalone,
}

// initializeStandaloneConfigCmd initializes config for standalone mode
var initializeStandaloneConfigCmd = &cobra.Command{
	Use:   "init-config",
	Short: "create a new default standalone-config",
	RunE: func(_ *cobra.Command, _ []string) error {
		path := cfg
		if path == "" {
			path = defaultStandaloneCfgFile
		}
		if err := checkExistenceOf(path); err != nil {
			return err
		}
		return ltoml.WriteConfig(path, config.NewDefaultStandaloneTOML())
	},
}

// serveStandalone runs the cluster as standalone mode
func serveStandalone(_ *cobra.Command, _ []string) error {
	ctx := newCtxWithSignals()

	standaloneCfg := config.Standalone{}
	if fileutil.Exist(cfg) || fileutil.Exist(defaultStandaloneCfgFile) {
		if err := config.LoadAndSetStandAloneConfig(cfg, defaultStandaloneCfgFile, &standaloneCfg); err != nil {
			return err
		}
	} else {
		standaloneCfg = config.NewDefaultStandalone()
	}

	if err := logger.InitLogger(standaloneCfg.Logging, standaloneLogFileName); err != nil {
		return fmt.Errorf("init logger error: %s", err)
	}
	if err := logger.InitAccessLogger(standaloneCfg.Logging, logger.AccessLogFileName); err != nil {
		return fmt.Errorf("init http access logger error: %s", err)
	}
	if err := logger.InitSlowSQLLogger(standaloneCfg.Logging, logger.SlowSQLLogFileName); err != nil {
		return fmt.Errorf("init logger error: %s", err)
	}

	// run cluster as standalone mode
	runtime := standalone.NewStandaloneRuntime(config.Version, &standaloneCfg, embedEtcd)
	return run(ctx, runtime, func() error {
		if !fileutil.Exist(cfg) && !fileutil.Exist(defaultStorageCfgFile) {
			return nil
		}
		newStandaloneCfg := config.Standalone{}
		return config.LoadAndSetStandAloneConfig(cfg, defaultStandaloneCfgFile, &newStandaloneCfg)
	})
}
