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

// Collector of perf events for a container.
package perf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"

	info "github.com/google/cadvisor/info/v1"
	"github.com/google/cadvisor/stats"
)

type buffer struct {
	*bytes.Buffer
}

func (b buffer) Close() error {
	return nil
}

func TestCollector_UpdateStats(t *testing.T) {
	collector := collector{uncore: &stats.NoopCollector{}}
	notScaledBuffer := buffer{bytes.NewBuffer([]byte{})}
	scaledBuffer := buffer{bytes.NewBuffer([]byte{})}
	groupedBuffer := buffer{bytes.NewBuffer([]byte{})}
	err := binary.Write(notScaledBuffer, binary.LittleEndian, GroupReadFormat{
		Nr:          1,
		TimeEnabled: 100,
		TimeRunning: 100,
	})
	assert.NoError(t, err)
	err = binary.Write(notScaledBuffer, binary.LittleEndian, Values{
		Value: 123456789,
		ID:    0,
	})
	assert.NoError(t, err)
	err = binary.Write(scaledBuffer, binary.LittleEndian, GroupReadFormat{
		Nr:          1,
		TimeEnabled: 3,
		TimeRunning: 1,
	})
	assert.NoError(t, err)
	err = binary.Write(scaledBuffer, binary.LittleEndian, Values{
		Value: 333333333,
		ID:    2,
	})
	assert.NoError(t, err)
	err = binary.Write(groupedBuffer, binary.LittleEndian, GroupReadFormat{
		Nr:          2,
		TimeEnabled: 100,
		TimeRunning: 100,
	})
	assert.NoError(t, err)
	err = binary.Write(groupedBuffer, binary.LittleEndian, Values{
		Value: 123456,
		ID:    0,
	})
	assert.NoError(t, err)
	err = binary.Write(groupedBuffer, binary.LittleEndian, Values{
		Value: 654321,
		ID:    1,
	})
	assert.NoError(t, err)

	collector.cpuFiles = map[int]group{
		1: {
			cpuFiles: map[string]map[int]readerCloser{
				"instructions": {0: notScaledBuffer},
			},
			names:      []string{"instructions"},
			leaderName: "instructions",
		},
		2: {
			cpuFiles: map[string]map[int]readerCloser{
				"cycles": {11: scaledBuffer},
			},
			names:      []string{"cycles"},
			leaderName: "cycles",
		},
		3: {
			cpuFiles: map[string]map[int]readerCloser{
				"cache-misses": {
					0: groupedBuffer,
				},
			},
			names:      []string{"cache-misses", "cache-references"},
			leaderName: "cache-misses",
		},
	}

	stats := &info.ContainerStats{}
	err = collector.UpdateStats(stats)

	assert.NoError(t, err)
	assert.Len(t, stats.PerfStats, 4)

	assert.Contains(t, stats.PerfStats, info.PerfStat{
		ScalingRatio: 0.3333333333333333,
		Value:        999999999,
		Name:         "cycles",
		Cpu:          11,
	})
	assert.Contains(t, stats.PerfStats, info.PerfStat{
		ScalingRatio: 1,
		Value:        123456789,
		Name:         "instructions",
		Cpu:          0,
	})
	assert.Contains(t, stats.PerfStats, info.PerfStat{
		ScalingRatio: 1.0,
		Value:        123456,
		Name:         "cache-misses",
		Cpu:          0,
	})
	assert.Contains(t, stats.PerfStats, info.PerfStat{
		ScalingRatio: 1.0,
		Value:        654321,
		Name:         "cache-references",
		Cpu:          0,
	})
}

func TestCreatePerfEventAttr(t *testing.T) {
	event := CustomEvent{
		Type:   0x1,
		Config: Config{uint64(0x2), uint64(0x3), uint64(0x4)},
		Name:   "fake_event",
	}

	attributes := createPerfEventAttr(event)

	assert.Equal(t, uint32(1), attributes.Type)
	assert.Equal(t, uint64(2), attributes.Config)
	assert.Equal(t, uint64(3), attributes.Ext1)
	assert.Equal(t, uint64(4), attributes.Ext2)
}

func TestSetGroupAttributes(t *testing.T) {
	event := CustomEvent{
		Type:   0x1,
		Config: Config{uint64(0x2), uint64(0x3), uint64(0x4)},
		Name:   "fake_event",
	}

	attributes := createPerfEventAttr(event)
	setGroupAttributes(attributes, true)
	assert.Equal(t, uint64(65536), attributes.Sample_type)
	assert.Equal(t, uint64(0xf), attributes.Read_format)
	assert.Equal(t, uint64(0x100003), attributes.Bits)

	attributes = createPerfEventAttr(event)
	setGroupAttributes(attributes, false)
	assert.Equal(t, uint64(65536), attributes.Sample_type)
	assert.Equal(t, uint64(0xf), attributes.Read_format)
	assert.Equal(t, uint64(0x100002), attributes.Bits)
}

func TestNewCollector(t *testing.T) {
	perfCollector := newCollector("cgroup", PerfEvents{
		Core: Events{
			Events: []Group{{[]Event{"event_1"}, false}, {[]Event{"event_2"}, false}},
			CustomEvents: []CustomEvent{{
				Type:   0,
				Config: []uint64{1, 2, 3},
				Name:   "event_2",
			}},
		},
	}, 1, []info.Node{})
	assert.Len(t, perfCollector.eventToCustomEvent, 1)
	assert.Nil(t, perfCollector.eventToCustomEvent[Event("event_1")])
	assert.Same(t, &perfCollector.events.Core.CustomEvents[0], perfCollector.eventToCustomEvent[Event("event_2")])
}
