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

// Utilities.
package resctrl

import (
	"fmt"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/fscommon"
	"github.com/opencontainers/runc/libcontainer/intelrdt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ControlGroup = "CTRL_MON"
	MonGroup = "MON"
	TASKS_FILENAME = "tasks"
	CPU_CGROUP = "cpu"
	ROOT_RESCTRL = "/sys/fs/resctrl"
)

func GetResctrlPath(containerName string) (string, error) {
	if containerName == "/" {
		return intelrdt.GetIntelRdtPath(containerName, ControlGroup)
	}

	pids, err := getPids(containerName)
	if err != nil {
		return "", err
	}

	if len(pids) == 0 {
		return "", fmt.Errorf("couldn't determine resctrl for %q: there is no pids in cGroup", containerName)
	}

	path, err := findControlGroup(pids)
	if err != nil {
		return "", err
	}

	properContainerName := strings.Replace(containerName[1:], "/", "-", -1)
	monGroupPath := filepath.Join(path, intelrdt.MonitoringGroupRoot, properContainerName)

	err = os.Mkdir(monGroupPath, os.ModePerm)
	if err != nil {
		return "", err
	}

	for _, pid := range pids {
		processThreads, err := intelrdt.GetAllProcessPids(pid)
		if err != nil {
			return "", err
		}
		for _, thread := range processThreads {
			err = intelrdt.WriteIntelRdtTasks(monGroupPath, thread)
			if err != nil {
				secondError := os.Remove(monGroupPath)
				if secondError != nil {
					return "", fmt.Errorf("%v : %v", err, secondError)
				}
				return "", err
			}
		}
	}

	return monGroupPath, nil
}

func getPids(containerName string) ([]int, error) {
	var pidsPath string
	if cgroups.IsCgroup2UnifiedMode() {
		pidsPath = filepath.Join("/sys/fs/cgroup", containerName)
	} else {
		pidsPath = filepath.Join("/sys/fs/cgroup", CPU_CGROUP, containerName)
	}

	return cgroups.GetAllPids(pidsPath)
}

func findControlGroup(pids []int) (string, error){
	availablePaths, err := filepath.Glob("/sys/fs/resctrl/*")
	if err != nil {
		return "", err
	}

	for _, path := range availablePaths {
		switch path {
		case filepath.Join(ROOT_RESCTRL, "cpus"):
			continue
		case filepath.Join(ROOT_RESCTRL, "cpus_list"):
			continue
		case filepath.Join(ROOT_RESCTRL, "info"):
			continue
		case filepath.Join(ROOT_RESCTRL, "mon_data"):
			continue
		case filepath.Join(ROOT_RESCTRL, "mon_groups"):
			continue
		case filepath.Join(ROOT_RESCTRL, "schemata"):
			continue
		case filepath.Join(ROOT_RESCTRL, "tasks"):
			inGroup, err := checkControlGroup(ROOT_RESCTRL, pids)
			if err != nil {
				return "", err
			}
			if inGroup {
				return ROOT_RESCTRL, nil
			}
		default:
			inGroup, err := checkControlGroup(path, pids)
			if err != nil {
				return "", err
			}
			if inGroup {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("there is no available control group")
}

func checkControlGroup(path string, pids []int) (bool,error) {
	taskFile, err := fscommon.ReadFile(path, "tasks")
	if err != nil {
		return false, err
	}

	for _, pid := range pids {
		if !strings.Contains(taskFile, strconv.Itoa(pid)) {
			return false, nil
		}
	}

	return true, nil
}
