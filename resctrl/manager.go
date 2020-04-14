// +build linux

package resctrl

import (
	"github.com/google/cadvisor/stats"
)

type manager struct {
	id string
	stats.NoopDestroy
}

func (m manager) GetCollector(resctrlPath string) (stats.Collector, error) {
	collector := newCollector(m.id, resctrlPath)
	return collector, nil
}

func NewManager(id string) (stats.Manager, error) {
	return &manager{id: id}, nil
}
