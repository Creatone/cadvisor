package resctrl

import (
	info "github.com/google/cadvisor/info/v1"
	"github.com/google/cadvisor/stats"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/intelrdt"
)

type collector struct {
	resctrlManager intelrdt.IntelRdtManager
	stats.NoopDestroy
}

func newCollector(id string, resctrlPath string) *collector {
	resctrlManager := intelrdt.IntelRdtManager{
		Config: &configs.Config{
			IntelRdt: &configs.IntelRdt{},
		},
		Id:   id,
		Path: resctrlPath,
	}

	return &collector{resctrlManager: resctrlManager}
}

func (c *collector) UpdateStats(stats *info.ContainerStats) error {
	stats.Resctrl = info.ResctrlStats{}

	resctrlStats, err := c.resctrlManager.GetStats()
	if err != nil {
		return err
	}

	numberOfNUMANodes := len(*resctrlStats.MBMStats)
	stats.Resctrl.MemoryBandwidthMonitoring = make([]info.MemoryBandwidthMonitoringStats, numberOfNUMANodes)
	for index, numaNodeStats := range *resctrlStats.MBMStats {
		stats.Resctrl.MemoryBandwidthMonitoring[index].TotalBytes = numaNodeStats.MBMTotalBytes
		stats.Resctrl.MemoryBandwidthMonitoring[index].LocalBytes = numaNodeStats.MBMLocalBytes
		stats.Resctrl.MemoryBandwidthMonitoring[index].LLCOccupancy = numaNodeStats.LLCOccupancy
	}

	return nil
}
