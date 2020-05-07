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

// Uncore perf events logic tests.
package perf

import (
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func mockSystemDevices() (string, error) {
	testDir, err := ioutil.TempDir("", "uncore_imc_test")
	if err != nil {
		return "", err
	}

	// First Uncore IMC PMU.
	firstPMUPath := filepath.Join(testDir, "uncore_imc_0")
	err = os.MkdirAll(firstPMUPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(filepath.Join(firstPMUPath, "cpumask"), []byte("0-1"), 777)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(filepath.Join(firstPMUPath, "type"), []byte("18"), 777)
	if err != nil {
		return "", err
	}

	// Second Uncore IMC PMU.
	secondPMUPath := filepath.Join(testDir, "uncore_imc_1")
	err = os.MkdirAll(secondPMUPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(filepath.Join(secondPMUPath, "cpumask"), []byte("0,1"), 777)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(filepath.Join(secondPMUPath, "type"), []byte("19"), 777)
	if err != nil {
		return "", err
	}

	return testDir, nil
}

func TestUncore(t *testing.T) {
	path, err := mockSystemDevices()
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(path)
		assert.Nil(t, err)
	}()

	actual, err := getUncoreIMCPMUs(path)
	assert.Nil(t, err)
	expected := uncorePMUs{
		{name: "uncore_imc_0", typeOf: 18, cpus: []uint32{0, 1}},
		{name: "uncore_imc_1", typeOf: 19, cpus: []uint32{0, 1}},
	}
	assert.Equal(t, expected, actual)

	actualCpus, err := actual.getCpus(expected[0].typeOf)
	assert.Nil(t, err)
	assert.Equal(t, expected[0].cpus, actualCpus)
}
