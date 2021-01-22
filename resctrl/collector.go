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
	"os"
	"time"

	"k8s.io/klog/v2"

	info "github.com/google/cadvisor/info/v1"
)

type collector struct {
	id          string
	resctrlPath string
	interval    time.Duration
	running     bool
}

func newCollector(id string, interval time.Duration) *collector {
	return &collector{id: id, interval: interval}
}

func (c *collector) setup() error {
	path, err := getResctrlPath(c.id)
	c.resctrlPath = path
	c.running = true

	if c.interval != 0 && c.id != rootContainer {
		if err != nil {
			klog.Error("Failed to setup container %q: %v", c.id, err)
			klog.Info("Trying again in next intervals!")
		}
		go func() {
			for {
				if c.running {
					klog.Infof("Interval for: %q", c.resctrlPath)
					c.prepareMonGroup()
				} else {
					c.clear()
					klog.Infof("Clear: %q", c.resctrlPath)
					break
				}
				time.Sleep(c.interval)
			}
		}()
	} else {
		if err != nil {
			c.running = false
			return err
		}
	}

	return nil
}

func (c *collector) prepareMonGroup() {
	newPath, err := getResctrlPath(c.id)
	if err != nil {
		klog.Error(err, " | couldn't update mon_group", c.id, c.resctrlPath)
	}

	if newPath != c.resctrlPath {
		err = os.RemoveAll(c.resctrlPath)
		if err != nil {
			klog.Error(err, " | couldn't update mon_group", c.id, c.resctrlPath)
		}
		c.resctrlPath = newPath
	}
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
	klog.Infof("Destroying %q", c.id)
	c.running = false
	c.clear()
}

func (c *collector) clear() {
	if c.id != rootContainer {
		err := os.RemoveAll(c.resctrlPath)
		if err != nil {
			klog.Errorf("Couldn't destroy container %v: %v", c.id, err)
		}
	}
}
