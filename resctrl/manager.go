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
	"github.com/google/cadvisor/stats"
	"sync"

	"k8s.io/klog/v2"

	"github.com/opencontainers/runc/libcontainer/intelrdt"
)

type manager struct {
	stats.NoopDestroy
	mu sync.Mutex
}

func (m *manager) GetCollector(containerName string) (stats.Collector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	resctrlPath, err := getResctrlPath(containerName)
	if err != nil {
		klog.V(4).Infof("Error getting resctrl path: %q", err)
		return &stats.NoopCollector{}, err
	}

	return newCollector(containerName, resctrlPath), nil
}

func NewManager(_ string) (stats.Manager, error) {
	if intelrdt.IsMBMEnabled() || intelrdt.IsCMTEnabled() {
		return &manager{mu: sync.Mutex{}}, nil
	}

	return &stats.NoopManager{}, nil
}
