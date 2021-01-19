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

// Collector tests.
package resctrl

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	info "github.com/google/cadvisor/info/v1"
)

func TestNewCollector(t *testing.T) {
	expectedId := "id"
	expectedResctrlPath := "path"
	collector := newCollector(expectedId, expectedResctrlPath)

	assert.Equal(t, collector.id, expectedId)
	assert.Equal(t, collector.resctrlPath, expectedResctrlPath)
}

func TestUpdateStats(t *testing.T) {
	rootResctrl = mockResctrl()
	defer os.RemoveAll(rootResctrl)

	pidsPath = mockContainersPids()
	defer os.RemoveAll(pidsPath)

	processPath = mockProcFs()
	defer os.RemoveAll(processPath)

	path, err := getResctrlPath("container")
	assert.NoError(t, err)

	mockResctrlMonData(path)
	enabledCMT, enabledMBM = true, true

	collector := newCollector("container", path)
	stats := info.ContainerStats{}

	// Write some dumb data.
	err = collector.UpdateStats(&stats)
	assert.NoError(t, err)
	assert.Equal(t, stats.Resctrl.Cache, []info.CacheStats{
		{LLCOccupancy: 1111},
		{LLCOccupancy: 3333},
	})
	assert.Equal(t, stats.Resctrl.MemoryBandwidth, []info.MemoryBandwidthStats{
		{
			TotalBytes: 3333,
			LocalBytes: 2222,
		},
		{
			TotalBytes: 3333,
			LocalBytes: 1111,
		},
	})
}

func TestDestroy(t *testing.T) {
	rootResctrl = mockResctrl()
	defer os.RemoveAll(rootResctrl)

	pidsPath = mockContainersPids()
	defer os.RemoveAll(pidsPath)

	processPath = mockProcFs()
	defer os.RemoveAll(processPath)

	path, err := getResctrlPath("container")
	assert.NoError(t, err)

	collector := newCollector("container")

	if stat, err := os.Stat(path); !stat.IsDir() || err != nil {
		t.Fail()
	}

	collector.Destroy()

	if stat, err := os.Stat(path); stat != nil && err != nil {
		t.Fail()
	}
}
