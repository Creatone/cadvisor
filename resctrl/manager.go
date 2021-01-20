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
	"sync"
	"time"

	"k8s.io/klog/v2"

	"github.com/google/cadvisor/stats"

	"github.com/opencontainers/runc/libcontainer/intelrdt"
)

type manager struct {
	stats.NoopDestroy
	interval       time.Duration
	collectors     []*collector
	collectorsLock sync.Mutex
}

func (m *manager) GetCollector(containerName string) (stats.Collector, error) {
	m.collectorsLock.Lock()
	defer m.collectorsLock.Unlock()
	collector, err := newCollector(containerName)
	if err != nil {
		return &stats.NoopCollector{}, err
	}

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
						klog.V(1).Infof("Trying: %q | %q", m.collectors[i].id, m.collectors[i].resctrlPath)
						m.collectors[i].prepareMonGroup()
						klog.V(1).Infof("Recreated: %q | %q", m.collectors[i].id, m.collectors[i].resctrlPath)
					} else {
						klog.V(1).Infof("Deleting: %q | %q", m.collectors[i].id, m.collectors[i].resctrlPath)
						m.collectors = append(m.collectors[:i], m.collectors[i+1:]...)
					}
				}
			}
		}()
	}
}

func NewManager(interval time.Duration) (stats.Manager, error) {
	if intelrdt.IsMBMEnabled() || intelrdt.IsCMTEnabled() {
		manager := &manager{interval: interval, collectorsLock: sync.Mutex{}}
		manager.handleInterval()
		return manager, nil
	}

	return &stats.NoopManager{}, nil
}
