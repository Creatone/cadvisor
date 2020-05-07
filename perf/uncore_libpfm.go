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

// Uncore perf events logic.
package perf

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	info "github.com/google/cadvisor/info/v1"
)

type pmu struct {
	name   string
	typeOf uint32
	cpus   []uint32
}

const (
	perfTypeHardware   uint32 = 0
	imcDirectoryName          = "uncore_imc"
	pmuTypeFilename           = "type"
	pmuCpumaskFilename        = "cpumask"
	systemDevicesPath         = "/sys/devices"
)

var imcPMUs uncorePMUs

func getIMCPMUs() (uncorePMUs, error) {
	if len(imcPMUs) == 0 {
		var err error
		imcPMUs, err = getUncoreIMCPMUs(systemDevicesPath)
		if err != nil {
			return nil, err
		}
	}

	return imcPMUs, nil
}

type uncorePMUs []pmu

func (pmus *uncorePMUs) getCpus(gotType uint32) ([]uint32, error) {
	for _, pmu := range *pmus {
		if pmu.typeOf == gotType {
			return pmu.cpus, nil
		}
	}

	return nil, fmt.Errorf("there is no pmu with event type: %#v", gotType)
}

// SetupPerfUncore gather information about Perf Uncore PMUs.
func SetupPerfUncore() error {
	var err error
	imcPMUs, err = getUncoreIMCPMUs(systemDevicesPath)
	if err != nil {
		return err
	}

	return nil
}

func getUncoreIMCPMUs(devicesPath string) (uncorePMUs, error) {
	pmus := uncorePMUs{}

	// Depends on platform, cpu mask could be for example in form "0-1" or "0,1".
	cpumaskRegexp := regexp.MustCompile("[-,\n]")
	err := filepath.Walk(devicesPath, func(path string, info os.FileInfo, err error) error {
		// Skip root path.
		if path == devicesPath {
			return nil
		}
		if info.IsDir() {
			if strings.Contains(info.Name(), imcDirectoryName) {
				buf, err := ioutil.ReadFile(filepath.Join(path, pmuTypeFilename))
				if err != nil {
					return err
				}
				typeString := strings.TrimSpace(string(buf))
				eventType, err := strconv.ParseUint(typeString, 0, 32)
				if err != nil {
					return err
				}

				buf, err = ioutil.ReadFile(filepath.Join(path, pmuCpumaskFilename))
				if err != nil {
					return err
				}
				var cpus []uint32
				cpumask := strings.TrimSpace(string(buf))
				for _, cpu := range cpumaskRegexp.Split(cpumask, -1) {
					parsedCPU, err := strconv.ParseUint(cpu, 0, 32)
					if err != nil {
						return err
					}
					cpus = append(cpus, uint32(parsedCPU))
				}

				pmus = append(pmus, pmu{name: info.Name(), typeOf: uint32(eventType), cpus: cpus})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return pmus, nil
}

type uncoreCollector struct {
	cpuFiles           map[string]map[uint32]map[int]readerCloser
	cpuFilesLock       sync.Mutex
	eventToCustomEvent map[Event]*CustomEvent
}

func NewUncoreCollector(perfEventConfig string) (*uncoreCollector, error) {
	return uncoreCollector{}, nil
}

func (u *uncoreCollector) Destroy() {
	//TODO: Do it.
}

func (u *uncoreCollector) GetStats() ([]info.PerfStat, error) {
	// TODO: Do it.
}
