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

// #cgo CFLAGS: -I/usr/include
// #cgo LDFLAGS: -lpfm
// #include <perfmon/pfmlib.h>
// #include <stdlib.h>
import "C"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"unsafe"

	info "github.com/google/cadvisor/info/v1"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

type collector struct {
	cgroupPath         string
	events             Events
	cpuFiles           map[string]map[uint32]map[int]readerCloser
	cpuFilesLock       sync.Mutex
	numCores           int
	eventToCustomEvent map[Event]*CustomEvent
}

var (
	isLibpfmInitialized = false
	libpmfMutex         = sync.Mutex{}
)

func init() {
	libpmfMutex.Lock()
	defer libpmfMutex.Unlock()
	pErr := C.pfm_initialize()
	if pErr != C.PFM_SUCCESS {
		fmt.Printf("unable to initialize libpfm: %d", int(pErr))
		return
	}
	isLibpfmInitialized = true
}

func newCollector(cgroupPath string, events Events, numCores int) *collector {
	collector := &collector{cgroupPath: cgroupPath, events: events, cpuFiles: map[string]map[uint32]map[int]readerCloser{}, numCores: numCores}
	mapEventsToCustomEvents(collector)
	return collector
}

func (c *collector) UpdateStats(stats *info.ContainerStats) error {
	c.cpuFilesLock.Lock()
	defer c.cpuFilesLock.Unlock()

	stats.PerfStats = []info.PerfStat{}
	klog.V(5).Infof("Attempting to update perf_event stats from cgroup %q", c.cgroupPath)
	for name, pmus := range c.cpuFiles {
		for pmu, files := range pmus {
			for cpu, file := range files {
				buf := make([]byte, 32)
				_, err := file.Read(buf)
				if err != nil {
					klog.Warningf("Unable to read from perf_event file (event: %q, CPU: %d) for %q", name, cpu, c.cgroupPath)
					continue
				}
				perfData := &ReadFormat{}
				reader := bytes.NewReader(buf)
				err = binary.Read(reader, binary.LittleEndian, perfData)
				if err != nil {
					klog.Warningf("Unable to decode from binary format read from perf_event file (event: %q, CPU: %d) for %q", name, cpu, c.cgroupPath)
					continue
				}
				klog.V(5).Infof("Read metric for event %q for cpu %d from cgroup %q: %d", name, cpu, c.cgroupPath, perfData.Value)
				scalingRatio := 1.0
				if perfData.TimeEnabled != 0 {
					scalingRatio = float64(perfData.TimeRunning) / float64(perfData.TimeEnabled)
				}
				stat := info.PerfStat{
					Value:        uint64(float64(perfData.Value) / scalingRatio),
					Name:         name,
					ScalingRatio: scalingRatio,
					Cpu:          cpu,
					Type:         pmu,
				}
				stats.PerfStats = append(stats.PerfStats, stat)
			}
		}
	}
	return nil
}

func (c *collector) setup() error {
	cgroup, err := os.Open(c.cgroupPath)
	if err != nil {
		return fmt.Errorf("unable to open cgroup directory %s: %s", c.cgroupPath, err)
	}
	defer cgroup.Close()

	c.cpuFilesLock.Lock()
	defer c.cpuFilesLock.Unlock()
	cgroupFd := int(cgroup.Fd())
	for _, group := range c.events.Events {
		customEvent, ok := c.eventToCustomEvent[group[0]]
		var err error
		if ok {
			err = c.setupRawNonGrouped(customEvent, cgroupFd)
		} else {
			err = c.setupNonGrouped(string(group[0]), cgroupFd)
		}
		if err != nil {
			return err
		}
	}

	for _, group := range c.events.UncoreEvents.MemoryController {
		customEvent, ok := c.eventToCustomEvent[group[0]]
		var err error
		if ok {
			err = c.setupRawNonGroupedUncoreMemoryController(customEvent, cgroupFd)
		} else {
			err = c.setupNonGroupedUncoreMemoryController(string(group[0]), cgroupFd)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *collector) setupRawNonGrouped(event *CustomEvent, cgroup int) error {
	klog.V(5).Infof("Setting up non-grouped raw perf event %#v", event)
	config := createPerfEventAttr(*event)
	err := c.registerEvent(config, string(event.Name), cgroup)
	if err != nil {
		return err
	}

	return nil
}

func (c *collector) registerEvent(config *unix.PerfEventAttr, name string, pid int) error {
	var cpu int
	for cpu = 0; cpu < c.numCores; cpu++ {
		groupFd, flags := -1, unix.PERF_FLAG_FD_CLOEXEC|unix.PERF_FLAG_PID_CGROUP
		fd, err := unix.PerfEventOpen(config, pid, cpu, groupFd, flags)
		if err != nil {
			return fmt.Errorf("setting up perf event %#v failed: %q", config, err)
		}
		perfFile := os.NewFile(uintptr(fd), name)
		if perfFile == nil {
			return fmt.Errorf("unable to create os.File from file descriptor %#v", fd)
		}

		c.addEventFile(name, config.Type, cpu, perfFile)
	}
	return nil
}

func (c *collector) addEventFile(name string, pmu uint32, cpu int, perfFile *os.File) {
	_, ok := c.cpuFiles[name]
	if !ok {
		c.cpuFiles[name] = map[uint32]map[int]readerCloser{}
	}

	_, ok = c.cpuFiles[name][pmu]
	if !ok {
		c.cpuFiles[name][pmu] = map[int]readerCloser{}
	}

	c.cpuFiles[name][pmu][cpu] = perfFile
}

func (c *collector) setupNonGrouped(name string, cgroup int) error {
	perfEventAttr, err := getPerfEventAttr(name)
	if err != nil {
		return err
	}
	defer C.free(unsafe.Pointer(perfEventAttr))

	return c.registerEvent(perfEventAttr, name, cgroup)
}

func createPerfEventAttr(event CustomEvent) *unix.PerfEventAttr {
	length := len(event.Config)

	config := &unix.PerfEventAttr{
		Type:   event.Type,
		Config: event.Config[0],
	}
	if length >= 2 {
		config.Ext1 = event.Config[1]
	}
	if length == 3 {
		config.Ext2 = event.Config[2]
	}

	setAttributes(config)
	klog.V(5).Infof("perf_event_attr struct prepared: %#v", config)
	return config
}

func (c *collector) setupRawNonGroupedUncoreMemoryController(event *CustomEvent, cgroup int) error {
	klog.V(5).Infof("Setting up non-grouped raw perf uncore memory controller event %#v", event)

	pmus, err := getIMCPMUs()
	if err != nil {
		return err
	}

	if event.Type == 0 {
		// PMU isn't set. Register event for all memory PMUs.
		for _, pmu := range pmus {
			newEvent := CustomEvent{
				Type:   pmu.typeOf,
				Config: event.Config,
				Name:   event.Name,
			}
			config := createPerfEventAttr(newEvent)
			err := c.registerUncoreMemoryControllerEvent(config, string(newEvent.Name), cgroup, pmu.cpus)
			if err != nil {
				return err
			}
		}
		return nil
	} else {
		// Register event for the PMU.
		config := createPerfEventAttr(*event)
		cpus, err := pmus.getCpus(event.Type)
		if err != nil {
			return err
		}
		return c.registerUncoreMemoryControllerEvent(config, string(event.Name), cgroup, cpus)
	}
}

func (c *collector) registerUncoreMemoryControllerEvent(config *unix.PerfEventAttr, name string, pid int, cpus []uint32) error {
	for _, cpu := range cpus {
		groupFd, flags := -1, unix.PERF_FLAG_FD_CLOEXEC|unix.PERF_FLAG_PID_CGROUP
		fd, err := unix.PerfEventOpen(config, pid, int(cpu), groupFd, flags)
		if err != nil {
			return fmt.Errorf("setting up perf event %#v failed: %q", config, err)
		}
		perfFile := os.NewFile(uintptr(fd), name)
		if perfFile == nil {
			return fmt.Errorf("unable to create os.File from file descriptor %#v", fd)
		}

		c.addEventFile(name, config.Type, int(cpu), perfFile)
	}

	return nil
}

func getPerfEventAttr(name string) (*unix.PerfEventAttr, error) {
	if !isLibpfmInitialized {
		return nil, fmt.Errorf("libpfm4 is not initialized, cannot proceed with setting perf events up")
	}

	perfEventAttrMemory := C.malloc(C.ulong(unsafe.Sizeof(unix.PerfEventAttr{})))
	event := pfmPerfEncodeArgT{}

	perfEventAttr := (*unix.PerfEventAttr)(perfEventAttrMemory)
	fstr := C.CString("")
	event.fstr = unsafe.Pointer(fstr)
	event.attr = perfEventAttrMemory
	event.size = C.ulong(unsafe.Sizeof(event))

	cSafeName := C.CString(name)

	pErr := C.pfm_get_os_event_encoding(cSafeName, C.PFM_PLM0|C.PFM_PLM3, C.PFM_OS_PERF_EVENT_EXT, unsafe.Pointer(&event))
	if pErr != C.PFM_SUCCESS {
		return nil, fmt.Errorf("unable to transform event name %s to perf_event_attr: %v", name, int(pErr))
	}

	klog.V(5).Infof("perf_event_attr: %#v", perfEventAttr)

	setAttributes(perfEventAttr)

	return perfEventAttr, nil
}

func (c *collector) setupNonGroupedUncoreMemoryController(name string, cgroup int) error {
	perfEventAttr, err := getPerfEventAttr(name)
	if err != nil {
		return err
	}
	defer C.free(unsafe.Pointer(perfEventAttr))

	klog.V(5).Infof("Setting up non-grouped uncore perf event %s", name)

	imcPMUs, err := getIMCPMUs()
	if err != nil {
		return err
	}

	if perfEventAttr.Type == perfTypeHardware {
		// Register event for all memory controllers.
		for _, pmu := range imcPMUs {
			perfEventAttr.Type = pmu.typeOf
			cpus, err := imcPMUs.getCpus(perfEventAttr.Type)
			if err != nil {
				return err
			}
			err = c.registerUncoreMemoryControllerEvent(perfEventAttr, name, cgroup, cpus)
			if err != nil {
				return err
			}
		}
		return nil
	} else {
		// Register event only for the memory controller.
		cpus, err := imcPMUs.getCpus(perfEventAttr.Type)
		if err != nil {
			return err
		}

		return c.registerUncoreMemoryControllerEvent(perfEventAttr, name, cgroup, cpus)
	}
}

func setAttributes(config *unix.PerfEventAttr) {
	config.Sample_type = perfSampleIdentifier
	config.Read_format = unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_ID
	config.Bits = perfAttrBitsInherit | perfAttrBitsExcludeGuest
	config.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
}

func (c *collector) Destroy() {
	c.cpuFilesLock.Lock()
	defer c.cpuFilesLock.Unlock()

	for name, pmus := range c.cpuFiles {
		for pmu, files := range pmus {
			for cpu, file := range files {
				klog.V(5).Infof("Closing perf_event file descriptor for cgroup %q, event %q, type %q and CPU %d", c.cgroupPath, name, pmu, cpu)
				err := file.Close()
				if err != nil {
					klog.Warningf("Unable to close perf_event file descriptor for cgroup %q, event %q, type %q and CPU %d", c.cgroupPath, name, pmu, cpu)
				}
			}
		}

		delete(c.cpuFiles, name)
	}
}

// Finalize terminates libpfm4 to free resources.
func Finalize() {
	libpmfMutex.Lock()
	defer libpmfMutex.Unlock()

	klog.V(1).Info("Attempting to terminate libpfm4")
	if !isLibpfmInitialized {
		klog.V(1).Info("libpfm4 has not been initialized; not terminating.")
		return
	}

	C.pfm_terminate()
	isLibpfmInitialized = false
}

func mapEventsToCustomEvents(collector *collector) {
	collector.eventToCustomEvent = map[Event]*CustomEvent{}
	for key, event := range collector.events.CustomEvents {
		collector.eventToCustomEvent[event.Name] = &collector.events.CustomEvents[key]
	}
}
