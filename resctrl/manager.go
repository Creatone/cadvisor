// +build linux

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

// Manager of resctrl for containers.
package resctrl

import (
	"time"

	"github.com/google/cadvisor/stats"

	"github.com/opencontainers/runc/libcontainer/intelrdt"
)

type manager struct {
	stats.NoopDestroy
	interval   time.Duration
	collectors []*collector
}

func (m *manager) GetCollector(containerName string) (stats.Collector, error) {
	collector, err := newCollector(containerName)
	if err != nil {
		return &stats.NoopCollector{}, err
	}

	collector.prepareMonGroup()

	m.collectors = append(m.collectors, collector)

	return collector, nil
}

func (m *manager) handleInterval() {
	if m.interval != 0 {
		go func() {
			for {
				time.Sleep(m.interval)
				for i := 0; i < len(m.collectors); i++ {
					if m.collectors[i].running {
						m.collectors[i].prepareMonGroup()
					} else {
						m.collectors = append(m.collectors[:i], m.collectors[i+1:]...)
					}
				}
			}
		}()
	}
}

func NewManager(interval time.Duration) (stats.Manager, error) {
	if intelrdt.IsMBMEnabled() || intelrdt.IsCMTEnabled() {
		manager := &manager{interval: interval}
		manager.handleInterval()
		return manager, nil
	}

	return &stats.NoopManager{}, nil
}
