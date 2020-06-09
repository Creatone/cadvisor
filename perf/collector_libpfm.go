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

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"

	info "github.com/google/cadvisor/info/v1"
	"github.com/google/cadvisor/stats"
)

type collector struct {
	cgroupPath         string
	events             PerfEvents
	cpuFiles           map[int]group
	cpuFilesLock       sync.Mutex
	numCores           int
	eventToCustomEvent map[Event]*CustomEvent
	uncore             stats.Collector
}

type group struct {
	cpuFiles   map[string]map[int]readerCloser
	names      []string
	leaderName string
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

func newCollector(cgroupPath string, events PerfEvents, numCores int, topology []info.Node) *collector {
	collector := &collector{cgroupPath: cgroupPath, events: events, numCores: numCores, cpuFiles: map[int]group{}, uncore: NewUncoreCollector(cgroupPath, events, topology)}
	mapEventsToCustomEvents(collector)
	return collector
}

func (c *collector) UpdateStats(stats *info.ContainerStats) error {
	err := c.uncore.UpdateStats(stats)
	if err != nil {
		klog.Errorf("Failed to get uncore perf event stats: %v", err)
	}

	c.cpuFilesLock.Lock()
	defer c.cpuFilesLock.Unlock()

	stats.PerfStats = []info.PerfStat{}
	klog.V(5).Infof("Attempting to update perf_event stats from cgroup %q", c.cgroupPath)

	for _, group := range c.cpuFiles {
		for cpu, file := range group.cpuFiles[group.leaderName] {
			stat, err := readGroupPerfStat(file, group, cpu, c.cgroupPath)
			if err != nil {
				klog.Warningf("Unable to read from perf_event_file (event: %q, CPU: %d) for %q: %q", group.leaderName, cpu, c.cgroupPath, err.Error())
				continue
			}

			stats.PerfStats = append(stats.PerfStats, stat...)
		}
	}

	return nil
}

func readGroupPerfStat(file readerCloser, group group, cpu int, cgroupPath string) ([]info.PerfStat, error) {
	var perfStats []info.PerfStat
	// 24 bytes of GroupReadFormat struct.
	// 16 bytes of Values struct for each element in group.
	buf := make([]byte, 24+16*len(group.names))
	_, err := file.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("unable to read perf event group ( leader = %s ): %v", group.leaderName, err)
	}
	perfData := &GroupReadFormat{}
	reader := bytes.NewBuffer(buf[:24]) // First 24 bytes of GroupReadFormat.
	err = binary.Read(reader, binary.LittleEndian, perfData)
	if err != nil {
		return nil, fmt.Errorf("unable to decode perf event group ( leader = %s ): %v", group.leaderName, err)
	}
	values := make([]Values, perfData.Nr)

	reader = bytes.NewBuffer(buf[24:]) // Rest bytes of Values for each element in group.
	err = binary.Read(reader, binary.LittleEndian, values)

	scalingRatio := 1.0
	if perfData.TimeEnabled != 0 {
		scalingRatio = float64(perfData.TimeRunning) / float64(perfData.TimeEnabled)
	}

	for i, name := range group.names {
		klog.V(5).Infof("Read metric for event %q for cpu %d from cgroup %q: %d", name, cpu, cgroupPath, values[i].Value)
		stat := info.PerfStat{
			Value:        uint64(float64(values[i].Value) / scalingRatio),
			Name:         name,
			ScalingRatio: scalingRatio,
			Cpu:          cpu,
		}
		perfStats = append(perfStats, stat)
	}

	return perfStats, nil
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
	for i, group := range c.events.Core.Events {
		// CPUs file descriptors of group leader needed for perf_event_open.
		leaderFileDescriptors := new([]int)
		*leaderFileDescriptors = make([]int, c.numCores)
		for j, event := range group.events {
			// First element is group leader.
			groupLeader := j == 0
			customEvent, ok := c.eventToCustomEvent[event]
			if ok {
				err = c.setupRawEvent(customEvent, cgroupFd, i, leaderFileDescriptors, groupLeader)
			} else {
				err = c.setupEvent(string(event), cgroupFd, i, leaderFileDescriptors, groupLeader)
			}
		}

		// Group is prepared so we should reset and enable counting.
		for _, fd := range *leaderFileDescriptors {
			err = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_RESET, 0)
			err = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (c *collector) setupRawEvent(event *CustomEvent, cgroup int, index int, leaderFileDescriptors *[]int, leader bool) error {
	klog.V(5).Infof("Setting up grouped raw perf event %#v", event)

	config := createPerfEventAttr(*event)
	setGroupAttributes(config, leader)
	klog.V(5).Infof("perf_event_attr struct prepared: %#v", config)

	err := c.registerEvent(config, string(event.Name), cgroup, index, leaderFileDescriptors, leader)
	if err != nil {
		return err
	}
	return nil
}

func (c *collector) setupEvent(name string, cgroup int, index int, leaderFileDescriptors *[]int, leader bool) error {
	if !isLibpfmInitialized {
		return fmt.Errorf("libpfm4 is not initialized, cannot proceed with setting perf events up")
	}

	klog.V(5).Infof("Setting up grouped perf event %s", name)

	perfEventAttrMemory := C.malloc(C.ulong(unsafe.Sizeof(unix.PerfEventAttr{})))
	defer C.free(perfEventAttrMemory)
	err := readPerfEventAttr(name, perfEventAttrMemory)
	if err != nil {
		return err
	}
	perfEventAttr := (*unix.PerfEventAttr)(perfEventAttrMemory)

	setGroupAttributes(perfEventAttr, leader)

	klog.V(5).Infof("perf_event_attr: %#v", perfEventAttr)
	return c.registerEvent(perfEventAttr, name, cgroup, index, leaderFileDescriptors, leader)
}

func readPerfEventAttr(name string, perfEventAttrMemory unsafe.Pointer) error {
	event := pfmPerfEncodeArgT{}
	fstr := C.CString("")
	event.fstr = unsafe.Pointer(fstr)
	event.attr = perfEventAttrMemory
	event.size = C.ulong(unsafe.Sizeof(event))
	cSafeName := C.CString(name)
	pErr := C.pfm_get_os_event_encoding(cSafeName, C.PFM_PLM0|C.PFM_PLM3, C.PFM_OS_PERF_EVENT, unsafe.Pointer(&event))
	if pErr != C.PFM_SUCCESS {
		return fmt.Errorf("unable to transform event name %s to perf_event_attr: %d", name, int(pErr))
	}
	return nil
}

func (c *collector) registerEvent(config *unix.PerfEventAttr, name string, cgroup int, groupIndex int, leaderFileDescriptors *[]int, leader bool) error {
	for cpu := 0; cpu < c.numCores; cpu++ {
		var groupFD, pid, flags int
		if leader {
			// Leader should have this parameter set for -1.
			groupFD = -1
			pid = cgroup
			flags = unix.PERF_FLAG_FD_CLOEXEC | unix.PERF_FLAG_PID_CGROUP
		} else {
			// If not leader, groupFd should be set to leaders perf file fd.
			groupFD = (*leaderFileDescriptors)[cpu]
			pid = -1
			flags = unix.PERF_FLAG_FD_CLOEXEC
		}

		fd, err := unix.PerfEventOpen(config, pid, cpu, groupFD, flags)
		if err != nil {
			return fmt.Errorf("setting up perf event %#v failed: %q", config, err)
		}
		perfFile := os.NewFile(uintptr(fd), name)
		if perfFile == nil {
			return fmt.Errorf("unable to create os.File from file descriptor %#v", fd)
		}

		c.addEventFile(groupIndex, name, cpu, perfFile)

		// If group leader, save fd for others.
		if leader {
			(*leaderFileDescriptors)[cpu] = fd
		}
	}

	return nil
}

func (c *collector) addEventFile(index int, name string, cpu int, perfFile *os.File) {
	_, ok := c.cpuFiles[index]
	if !ok {
		c.cpuFiles[index] = group{
			leaderName: name,
			cpuFiles:   map[string]map[int]readerCloser{},
		}
	}

	_, ok = c.cpuFiles[index].cpuFiles[name]
	if !ok {
		c.cpuFiles[index].cpuFiles[name] = map[int]readerCloser{}
	}

	c.cpuFiles[index].cpuFiles[name][cpu] = perfFile

	// Check if name is already stored.
	for _, have := range c.cpuFiles[index].names {
		if name == have {
			return
		}
	}

	// Otherwise save it.
	c.cpuFiles[index] = group{
		cpuFiles:   c.cpuFiles[index].cpuFiles,
		names:      append(c.cpuFiles[index].names, name),
		leaderName: c.cpuFiles[index].leaderName,
	}
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

	klog.V(5).Infof("perf_event_attr struct prepared: %#v", config)
	return config
}

func setGroupAttributes(config *unix.PerfEventAttr, leader bool) {
	config.Sample_type = perfSampleIdentifier
	config.Read_format = unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_GROUP | unix.PERF_FORMAT_ID
	config.Bits = perfAttrBitsInherit | perfAttrBitsExcludeGuest

	// Group leader should have this flag set to disable counting until all group would be prepared.
	if leader {
		config.Bits |= unix.PerfBitDisabled
	}

	config.Size = uint32(unsafe.Sizeof(unix.PerfEventAttr{}))
}

func (c *collector) Destroy() {
	c.uncore.Destroy()
	c.cpuFilesLock.Lock()
	defer c.cpuFilesLock.Unlock()

	for _, group := range c.cpuFiles {
		for name, files := range group.cpuFiles {
			for cpu, file := range files {
				klog.V(5).Infof("Closing perf_event file descriptor for cgroup %q, event %q and CPU %d", c.cgroupPath, name, cpu)
				err := file.Close()
				if err != nil {
					klog.Warningf("Unable to close perf_event file descriptor for cgroup %q, event %q and CPU %d", c.cgroupPath, name, cpu)
				}
			}
			delete(group.cpuFiles, name)
		}
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
	for key, event := range collector.events.Core.CustomEvents {
		collector.eventToCustomEvent[event.Name] = &collector.events.Core.CustomEvents[key]
	}
}
