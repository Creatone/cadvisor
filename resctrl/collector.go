// +build linux

// Copyright 2021 Google Inc. All Rights Reserved.
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
	"os"

	info "github.com/google/cadvisor/info/v1"
)

type collector struct {
	id string
	resctrlPath string
}

func newCollector(id string, resctrlPath string) *collector {
	collector := &collector{
		id: id,
		resctrlPath: resctrlPath,
	}

	return collector
}

func (c *collector) UpdateStats(stats *info.ContainerStats) error {
	stats.Resctrl = info.ResctrlStats{}

	resctrlStats, err := getStats(c.resctrlPath)
	if err != nil {
		return err
	}
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

	return nil
}

func (c *collector) Destroy() {
	if c.id != "/" {
		err := os.RemoveAll(c.resctrlPath)
		if err != nil {
			klog.Errorf("Couldn't destroy container %v: %v", c.id, err)
		}
	}
}
