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

// Collector of resctrl for a container.
package resctrl

import (
	"k8s.io/klog/v2"
	"path/filepath"

	info "github.com/google/cadvisor/info/v1"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/intelrdt"
)

const (
	rootContainerId = "/"
)

type collector struct {
	resctrl  intelrdt.IntelRdtManager
	pidsPath string
}

func newCollector(id string) (*collector, error) {
	if id == rootContainerId {
		collector := collector{
			intelrdt.IntelRdtManager{
				Id:   id,
				Type: intelrdt.CTRL_MON,
			}, ""}

		return &collector, nil
	}

	var pidsPath string
	if cgroups.IsCgroup2UnifiedMode() {
		pidsPath = filepath.Join("/sys/fs/cgroup", id)
	} else {
		pidsPath = filepath.Join("/sys/fs/cgroup/cpu", id)
	}

	collector := &collector{
		intelrdt.IntelRdtManager{
			Id:   id,
			Type: intelrdt.MON,
		}, pidsPath}

	// Prepare monitoring directory.
	err := collector.resctrl.Set(nil)
	if err != nil {
		return nil, err
	}

	// Prepare pids.
	err = collector.updatePids()
	if err != nil {
		return nil, err
	}

	return collector, nil
}

func (c *collector) updatePids() error {
	pids, err := cgroups.GetPids(c.pidsPath)
	if err != nil {
		return err
	}

	for _, pid := range pids {
		err := c.resctrl.Apply(pid)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *collector) UpdateStats(stats *info.ContainerStats) error {
	resctrlStats, err := c.resctrl.GetStats()
	if err != nil {
		return err
	}

	stats.Resctrl = info.ResctrlStats{}

	numberOfNUMANodes := len(*resctrlStats.MBMStats)

	stats.Resctrl.MemoryBandwidth = make([]info.MemoryBandwidthStats, 0, numberOfNUMANodes)
	stats.Resctrl.Cache = make([]info.CacheStats, 0, numberOfNUMANodes)

	for _, numaNodeStats := range *resctrlStats.MBMStats {
		stats.Resctrl.MemoryBandwidth = append(stats.Resctrl.MemoryBandwidth,
			info.MemoryBandwidthStats{
				TotalBytes: numaNodeStats.MBMTotalBytes,
				LocalBytes: numaNodeStats.MBMLocalBytes,
			})
	}

	for _, numaNodeStats := range *resctrlStats.CMTStats {
		stats.Resctrl.Cache = append(stats.Resctrl.Cache,
			info.CacheStats{LLCOccupancy: numaNodeStats.LLCOccupancy})
	}

	if c.resctrl.Id != rootContainerId {
		err = c.updatePids()
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *collector) Destroy() {
	// We shouldn't destroy Resctrl rootfs.
	if c.resctrl.Id != rootContainerId {
		err := c.resctrl.Destroy()
		if err != nil {
			klog.Error(err)
		}
	}
}
