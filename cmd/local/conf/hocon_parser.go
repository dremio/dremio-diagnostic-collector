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

package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/gurkankaymak/hocon"
)

// DremioHOCONConfig represents the parsed HOCON configuration for Dremio
type DremioHOCONConfig struct {
	config *hocon.Config
}

// NewDremioHOCONConfig creates a new DremioHOCONConfig from a file path
func NewDremioHOCONConfig(confFilePath string, dremioHome string) (*DremioHOCONConfig, error) {
	content, err := os.ReadFile(filepath.Clean(confFilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read dremio.conf: %w", err)
	}

	// Replace DREMIO_HOME placeholder with actual value
	contentStr := strings.ReplaceAll(string(content), "${DREMIO_HOME}", dremioHome)

	// Parse the HOCON content
	config, err := hocon.ParseString(contentStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dremio.conf as HOCON: %w", err)
	}

	return &DremioHOCONConfig{
		config: config,
	}, nil
}

// GetString returns a string value from the configuration
func (c *DremioHOCONConfig) GetString(path string) string {
	// Get the string from the config
	value := c.config.GetString(path)

	// Clean up the string by removing extra quotes
	value = strings.ReplaceAll(value, "\"\"\"", "")
	value = strings.Trim(value, "\"")

	return value
}

// GetBool returns a boolean value from the configuration
func (c *DremioHOCONConfig) GetBool(path string) bool {
	return c.config.GetBoolean(path)
}

// GetInt returns an integer value from the configuration
func (c *DremioHOCONConfig) GetInt(path string) int {
	return c.config.GetInt(path)
}

// GetFloat returns a float value from the configuration
func (c *DremioHOCONConfig) GetFloat(path string) float64 {
	return c.config.GetFloat64(path)
}

// HasPath checks if a path exists in the configuration
func (c *DremioHOCONConfig) HasPath(path string) bool {
	// The hocon library doesn't have a HasPath method, so we'll implement our own
	// by checking if GetString returns an empty string and there's no error
	return c.config.GetString(path) != "" || c.config.GetBoolean(path)
}

// IsCoordinatorMaster checks if this node is configured as a master coordinator
func (c *DremioHOCONConfig) IsCoordinatorMaster() bool {
	// Check if services.coordinator.master.enabled is true
	if c.HasPath("services.coordinator.master.enabled") {
		return c.GetBool("services.coordinator.master.enabled")
	}

	// Default to false if not specified
	return false
}

// IsCoordinator checks if this node is configured as a coordinator
func (c *DremioHOCONConfig) IsCoordinator() bool {
	if c.HasPath("services.coordinator.enabled") {
		return c.GetBool("services.coordinator.enabled")
	}
	// Default to false if not specified
	return false
}

// IsExecutor checks if this node is configured as an executor
func (c *DremioHOCONConfig) IsExecutor() bool {
	if c.HasPath("services.executor.enabled") {
		return c.GetBool("services.executor.enabled")
	}
	// Default to false if not specified
	return false
}

// GetRocksDBPath returns the path to the RocksDB directory
func (c *DremioHOCONConfig) GetRocksDBPath(dremioHome string) string {
	// First check for direct db path
	if c.HasPath("db") {
		return c.GetString("db")
	}

	// Then check for paths.db
	if c.HasPath("paths.db") {
		return c.GetString("paths.db")
	}

	// Check for paths.local
	var localPath string
	if c.HasPath("paths.local") {
		localPath = c.GetString("paths.local")
	} else {
		// Default local path
		localPath = filepath.Join(dremioHome, "data")
	}

	// Default RocksDB path
	return filepath.Join(localPath, "db")
}

// ParseDremioConf parses the dremio.conf file using the HOCON parser
func ParseDremioConf(dremioConfPath string, dremioHome string) (*DremioHOCONConfig, error) {
	config, err := NewDremioHOCONConfig(dremioConfPath, dremioHome)
	if err != nil {
		simplelog.Errorf("Failed to parse dremio.conf: %v", err)
		return nil, err
	}

	return config, nil
}
