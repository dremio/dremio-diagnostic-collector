//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package local implements the Collector interface for local (same-host) collection,
// with role auto-detection from dremio.conf.
package local

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// compile-time interface check
var _ collection.Collector = (*LocalCollector)(nil)

// LocalCollector implements collection.Collector for local (same-host) diagnostics.
// It detects the node role (coordinator vs executor) from dremio.conf and streams
// local files directly, with optional gzip compression.
type LocalCollector struct {
	cli           cli.CmdExecutor
	isCoordinator bool
}

// NewLocalCollector creates a LocalCollector. It parses confPath (dremio.conf) to
// determine the node role. If the conf file is unreadable or keys are missing, the
// role defaults to coordinator.
func NewLocalCollector(hook shutdown.CancelHook, confPath, dremioHome string) *LocalCollector {
	isCoordinator := detectRole(confPath, dremioHome)
	role := "coordinator"
	if !isCoordinator {
		role = "executor"
	}
	simplelog.Infof("LocalCollector: initialized with detected role=%s (confPath=%s)", role, confPath)
	return &LocalCollector{
		cli:           cli.NewCli(hook),
		isCoordinator: isCoordinator,
	}
}

// detectRole parses dremio.conf and determines whether the local node is a coordinator.
// Rules:
//   - If conf is unreadable or unparsable → coordinator (default)
//   - If services.coordinator.enabled key is absent AND services.executor.enabled is absent → coordinator
//   - If both coordinator.enabled=true and executor.enabled=true → coordinator
//   - If coordinator.enabled=false and executor.enabled=true → executor
//   - All other cases → coordinator
//
// Note: HasPath returns false for boolean-false values, so we use GetString to detect
// key presence and GetBool for the actual value.
func detectRole(confPath, dremioHome string) bool {
	cfg, err := conf.ParseDremioConf(confPath, dremioHome)
	if err != nil {
		simplelog.Warningf("LocalCollector: could not parse dremio.conf at %s: %v — defaulting to coordinator", confPath, err)
		return true
	}

	// Use GetString to detect whether the key actually exists in HOCON.
	// HasPath is unreliable for boolean false (GetString returns "" and GetBoolean returns false).
	coordStr := cfg.GetString("services.coordinator.enabled")
	execStr := cfg.GetString("services.executor.enabled")

	coordKeyPresent := coordStr != ""
	execKeyPresent := execStr != ""

	// Neither key present → default to coordinator
	if !coordKeyPresent && !execKeyPresent {
		simplelog.Infof("LocalCollector: no role keys in dremio.conf — defaulting to coordinator")
		return true
	}

	coordEnabled := cfg.GetBool("services.coordinator.enabled")
	execEnabled := cfg.GetBool("services.executor.enabled")

	// Both true or only coord true → coordinator
	// Only exec true and coord false → executor
	if execEnabled && !coordEnabled {
		return false
	}
	return true
}

func (c *LocalCollector) SetHostPid(_, _ string) {
	// not needed for local collection — normal cancellation works
}

func (c *LocalCollector) CleanupRemote() error {
	return nil
}

func (c *LocalCollector) Name() string {
	return "Local Collect"
}

func (c *LocalCollector) Protocol() string {
	return "Local"
}

func (c *LocalCollector) HelpText() string {
	return "local collection mode — running diagnostics directly on the Dremio host"
}

func (c *LocalCollector) HostExecuteAndStream(mask bool, _ string, output cli.OutputHandler, pat string, args ...string) error {
	// Wrap in sh -c to match SSH/kubectl behavior — callers use shell operators (&&, |, globs, etc.)
	shellCmd := strings.Join(args, " ")
	return c.cli.ExecuteAndStreamOutput(mask, output, pat, "sh", "-c", shellCmd)
}

func (c *LocalCollector) HostExecute(mask bool, _ string, args ...string) (string, error) {
	return cli.CollectOutput(c.HostExecuteAndStream, mask, "", args...)
}

func (c *LocalCollector) CopyToHost(_ string, source, destination string) (string, error) {
	src, err := os.Open(filepath.Clean(source))
	if err != nil {
		return "", err
	}
	defer src.Close() //nolint:errcheck // read-only file; close error is non-fatal
	dst, err := os.Create(filepath.Clean(destination))
	if err != nil {
		return "", err
	}
	defer dst.Close() //nolint:errcheck // best-effort close after copy
	_, err = io.Copy(dst, src)
	return "", err
}

func (c *LocalCollector) GetCoordinators() ([]string, error) {
	if !c.isCoordinator {
		return []string{}, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return []string{}, err
	}
	return []string{host}, nil
}

func (c *LocalCollector) GetExecutors() ([]string, error) {
	if c.isCoordinator {
		return []string{}, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return []string{}, err
	}
	return []string{host}, nil
}

// StreamFromHost streams a local file to writer. When useGzip is true, it uses
// "gzip -c <path>" to stream compressed data. When false, it reads the file directly.
func (c *LocalCollector) StreamFromHost(_, remotePath string, writer io.Writer, useGzip bool) error {
	if remotePath == "" {
		return fmt.Errorf("StreamFromHost: remotePath is empty")
	}

	if useGzip {
		simplelog.Infof("StreamFromHost: streaming %v with gzip", remotePath)
		// #nosec G204 -- remotePath is a discovered file path, not user input
		cmd := exec.Command("gzip", "-c", remotePath)
		cmd.Stdout = writer

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("StreamFromHost: failed to create stderr pipe for %v (gzip=true): %w", remotePath, err)
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("StreamFromHost: failed to start gzip for %v (gzip=true): %w", remotePath, err)
		}

		stderrBytes, _ := io.ReadAll(stderrPipe)

		if err := cmd.Wait(); err != nil {
			stderrMsg := strings.TrimSpace(string(stderrBytes))
			if stderrMsg != "" {
				return fmt.Errorf("StreamFromHost: gzip -c failed on %v (gzip=true): %w (stderr: %s)", remotePath, err, stderrMsg)
			}
			return fmt.Errorf("StreamFromHost: gzip -c failed on %v (gzip=true): %w", remotePath, err)
		}

		simplelog.Infof("StreamFromHost: completed streaming %v (gzip=true)", remotePath)
		return nil
	}

	simplelog.Infof("StreamFromHost: streaming %v (gzip=false)", remotePath)
	src, err := os.Open(filepath.Clean(remotePath))
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed to open %v (gzip=false): %w", remotePath, err)
	}
	defer src.Close() //nolint:errcheck // read-only file; close error is non-fatal
	_, err = io.Copy(writer, src)
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed to copy %v (gzip=false): %w", remotePath, err)
	}
	simplelog.Infof("StreamFromHost: completed streaming %v (gzip=false)", remotePath)
	return nil
}

// DiscoverFiles runs local discovery commands.
func (c *LocalCollector) DiscoverFiles(host, logDir, confDir string) (*collection.RemoteNodeInfo, error) {
	return collection.RunDiscovery(func(h string, args ...string) (string, error) {
		return c.HostExecute(false, h, args...)
	}, host, logDir, confDir)
}
