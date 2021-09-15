package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

const (
	nvidiaRuntime            = "nvidia-container-runtime"
	nvidiaHook               = "nvidia-container-runtime-hook"
	bundlePathSuffix         = "test/output/bundle/"
	specFile                 = "config.json"
	unmodifiedSpecFileSuffix = "test/input/test_spec.json"
)

type testConfig struct {
	root string
}

var cfg *testConfig

func TestMain(m *testing.M) {
	// TEST SETUP
	// Update PATH to execute mock runc in current directory
	_, filename, _, _ := runtime.Caller(0)

	var err error
	moduleRoot, err := getModuleRoot(filename)
	if err != nil {
		logger.Fatalf("error in test setup: could not get module root: %v", err)
	}

	paths := strings.Split(os.Getenv("PATH"), ":")
	paths = append([]string{moduleRoot}, paths...)
	os.Setenv("PATH", strings.Join(paths, ":"))

	// Confirm path setup correctly
	runcPath, err := exec.LookPath("runc")
	if err != nil || !strings.HasPrefix(runcPath, moduleRoot) {
		logger.Fatal("error in test setup: mock runc path set incorrectly in TestMain()")
	}

	cfg = &testConfig{
		root: moduleRoot,
	}

	// RUN TESTS
	exitCode := m.Run()

	// TEST CLEANUP
	os.Remove(specFile)

	os.Exit(exitCode)
}

func getModuleRoot(dir string) (string, error) {
	if dir == "" || dir == "/" {
		return "", fmt.Errorf("module root not found")
	}

	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		return getModuleRoot(filepath.Dir(dir))
	}

	// go.mod was found in dir
	return dir, nil
}

// case 1) nvidia-container-runtime run --bundle
// case 2) nvidia-container-runtime create --bundle
//		- Confirm the runtime handles bad input correctly
func TestBadInput(t *testing.T) {
	err := cfg.generateNewRuntimeSpec()
	if err != nil {
		t.Fatal(err)
	}

	cmdRun := exec.Command(nvidiaRuntime, "run", "--bundle")
	t.Logf("executing: %s\n", strings.Join(cmdRun.Args, " "))
	output, err := cmdRun.CombinedOutput()
	require.Errorf(t, err, "runtime should return an error", "output=%v", string(output))

	cmdCreate := exec.Command(nvidiaRuntime, "create", "--bundle")
	t.Logf("executing: %s\n", strings.Join(cmdCreate.Args, " "))
	err = cmdCreate.Run()
	require.Error(t, err, "runtime should return an error")
}

// case 1) nvidia-container-runtime run --bundle <bundle-name> <ctr-name>
//		- Confirm the runtime runs with no errors
// case 2) nvidia-container-runtime create --bundle <bundle-name> <ctr-name>
//		- Confirm the runtime inserts the NVIDIA prestart hook correctly
func TestGoodInput(t *testing.T) {
	err := cfg.generateNewRuntimeSpec()
	if err != nil {
		t.Fatalf("error generating runtime spec: %v", err)
	}

	cmdRun := exec.Command(nvidiaRuntime, "run", "--bundle", cfg.bundlePath(), "testcontainer")
	t.Logf("executing: %s\n", strings.Join(cmdRun.Args, " "))
	output, err := cmdRun.CombinedOutput()
	require.NoErrorf(t, err, "runtime should not return an error", "output=%v", string(output))

	// Check config.json and confirm there are no hooks
	spec, err := cfg.getRuntimeSpec()
	require.NoError(t, err, "should be no errors when reading and parsing spec from config.json")
	require.Empty(t, spec.Hooks, "there should be no hooks in config.json")

	cmdCreate := exec.Command(nvidiaRuntime, "create", "--bundle", cfg.bundlePath(), "testcontainer")
	t.Logf("executing: %s\n", strings.Join(cmdCreate.Args, " "))
	err = cmdCreate.Run()
	require.NoError(t, err, "runtime should not return an error")

	// Check config.json for NVIDIA prestart hook
	spec, err = cfg.getRuntimeSpec()
	require.NoError(t, err, "should be no errors when reading and parsing spec from config.json")
	require.NotEmpty(t, spec.Hooks, "there should be hooks in config.json")
	require.Equal(t, 1, nvidiaHookCount(spec.Hooks), "exactly one nvidia prestart hook should be inserted correctly into config.json")
}

// NVIDIA prestart hook already present in config file
func TestDuplicateHook(t *testing.T) {
	err := cfg.generateNewRuntimeSpec()
	if err != nil {
		t.Fatal(err)
	}

	var spec specs.Spec
	spec, err = cfg.getRuntimeSpec()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("inserting nvidia prestart hook to config.json")
	if err = addNVIDIAHook(&spec); err != nil {
		t.Fatal(err)
	}

	jsonOutput, err := json.MarshalIndent(spec, "", "\t")
	if err != nil {
		t.Fatal(err)
	}

	jsonFile, err := os.OpenFile(cfg.specFilePath(), os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = jsonFile.WriteAt(jsonOutput, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Test how runtime handles already existing prestart hook in config.json
	cmdCreate := exec.Command(nvidiaRuntime, "create", "--bundle", cfg.bundlePath(), "testcontainer")
	t.Logf("executing: %s\n", strings.Join(cmdCreate.Args, " "))
	output, err := cmdCreate.CombinedOutput()
	require.NoErrorf(t, err, "runtime should not return an error", "output=%v", string(output))

	// Check config.json for NVIDIA prestart hook
	spec, err = cfg.getRuntimeSpec()
	require.NoError(t, err, "should be no errors when reading and parsing spec from config.json")
	require.NotEmpty(t, spec.Hooks, "there should be hooks in config.json")
	require.Equal(t, 1, nvidiaHookCount(spec.Hooks), "exactly one nvidia prestart hook should be inserted correctly into config.json")
}

// addNVIDIAHook is a basic wrapper for nvidiaContainerRunime.addNVIDIAHook that is used for
// testing.
func addNVIDIAHook(spec *specs.Spec) error {
	r := nvidiaContainerRuntime{logger: logger.Logger}
	return r.addNVIDIAHook(spec)
}

func (c testConfig) getRuntimeSpec() (specs.Spec, error) {
	filePath := c.specFilePath()

	var spec specs.Spec
	jsonFile, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil {
		return spec, err
	}
	defer jsonFile.Close()

	jsonContent, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return spec, err
	} else if json.Valid(jsonContent) {
		err = json.Unmarshal(jsonContent, &spec)
		if err != nil {
			return spec, err
		}
	} else {
		err = json.NewDecoder(bytes.NewReader(jsonContent)).Decode(&spec)
		if err != nil {
			return spec, err
		}
	}

	return spec, err
}

func (c testConfig) bundlePath() string {
	return filepath.Join(c.root, bundlePathSuffix)
}

func (c testConfig) specFilePath() string {
	return filepath.Join(c.bundlePath(), specFile)
}

func (c testConfig) unmodifiedSpecFile() string {
	return filepath.Join(c.root, unmodifiedSpecFileSuffix)
}

func (c testConfig) generateNewRuntimeSpec() error {
	var err error

	err = os.MkdirAll(c.bundlePath(), 0755)
	if err != nil {
		return err
	}

	cmd := exec.Command("cp", c.unmodifiedSpecFile(), c.specFilePath())
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

// Return number of valid NVIDIA prestart hooks in runtime spec
func nvidiaHookCount(hooks *specs.Hooks) int {
	prestartHooks := hooks.Prestart
	count := 0

	for _, hook := range prestartHooks {
		if strings.Contains(hook.Path, nvidiaHook) {
			count++
		}
	}
	return count
}

func TestGetConfigWithCustomConfig(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	// By default debug is disabled
	contents := []byte("[nvidia-container-runtime]\ndebug = \"/nvidia-container-toolkit.log\"")
	testDir := filepath.Join(wd, "test")
	filename := filepath.Join(testDir, configFilePath)

	os.Setenv(configOverride, testDir)

	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0766))
	require.NoError(t, ioutil.WriteFile(filename, contents, 0766))

	defer func() { require.NoError(t, os.RemoveAll(testDir)) }()

	cfg, err := getConfig()
	require.NoError(t, err)
	require.Equal(t, cfg.debugFilePath, "/nvidia-container-toolkit.log")
}
