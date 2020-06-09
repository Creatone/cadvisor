// +build libpfm,cgo

// Copyright 2020 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Manager of perf events for containers.
package perf

import (
	"fmt"
	"os"

	info "github.com/google/cadvisor/info/v1"
	"github.com/google/cadvisor/stats"
)

type manager struct {
	events   PerfEvents
	numCores int
	topology []info.Node
	stats.NoopDestroy
}

func NewManager(configFile string, numCores int, topology []info.Node) (stats.Manager, error) {
	if configFile == "" {
		return &stats.NoopManager{}, nil
	}

	file, err := os.Open(configFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read configuration file %q: %q", configFile, err)
	}

	config, err := parseConfig(file)
	if err != nil {
		return nil, fmt.Errorf("unable to read configuration file %q: %q", configFile, err)
	}

	return &manager{events: config, numCores: numCores, topology: topology}, nil
}

func (m *manager) GetCollector(cgroupPath string) (stats.Collector, error) {
	collector := newCollector(cgroupPath, m.events, m.numCores, m.topology)
	err := collector.setup()
	if err != nil {
		collector.Destroy()
		return &stats.NoopCollector{}, err
	}
	return collector, nil
}
