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

// helpers package provides different functionality
package helpers

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

// fshelper provides functions to wrapper os file system calls
// to better facilitate testing
type Filesystem interface {
	Stat(name string) (os.FileInfo, error)
	Create(name string) (File, error)
	MkdirAll(path string, perm os.FileMode) error
	Mkdir(path string, perm os.FileMode) error
	MkdirTemp(name string, pattern string) (string, error)
	RemoveAll(path string) error
	Remove(name string) error
	WriteFile(name string, data []byte, perms os.FileMode) error
}

type File interface {
	Name() string
	Write(b []byte) (n int, err error)
	Sync() error
	Close() error
}

func NewRealFileSystem() *RealFileSystem {
	return &RealFileSystem{}
}

func NewFakeFileSystem() *FakeFileSystem {
	return &FakeFileSystem{}
}

const DirPerms fs.FileMode = 0750

// Real file
type RealFile struct {
	file *os.File
}

// Rea fileSystem wrapper
type RealFileSystem struct {
}

// Fake File
type FakeFile struct {
}

// Fake filesystem wrapper
type FakeFileSystem struct {
}

// Name
func (f *RealFile) Name() string {
	return f.file.Name()
}

// Write
func (f *RealFile) Write(b []byte) (n int, err error) {
	return f.file.Write(b)
}

// Sync
func (f *RealFile) Sync() error {
	return f.file.Sync()
}

// Close
func (f *RealFile) Close() error {
	return f.file.Close()
}

// Stat
func (f RealFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Create
func (f RealFileSystem) Create(name string) (File, error) {
	fd, err := os.Create(filepath.Clean(name))
	if err != nil {
		return nil, err
	}
	realFile := &RealFile{
		file: fd,
	}
	return realFile, nil
}

// Mkdir
func (f RealFileSystem) Mkdir(name string, perms os.FileMode) error {
	err := os.Mkdir(name, perms)
	return err
}

// MkdirTemp
func (f RealFileSystem) MkdirTemp(name string, pattern string) (dir string, err error) {
	if conf.KeyTmpOutputDir == "" {
		dir, err = os.MkdirTemp(name, pattern)
	} else {
		dir = conf.KeyTmpOutputDir
	}
	return dir, err
}

// MkdirAll
func (f RealFileSystem) MkdirAll(name string, perms os.FileMode) error {
	err := os.MkdirAll(name, perms)
	return err
}

// Remove
func (f RealFileSystem) Remove(path string) error {
	err := os.Remove(path)
	return err
}

// RemoveAll
func (f RealFileSystem) RemoveAll(path string) error {
	err := os.RemoveAll(path)
	return err
}

// WriteFile
func (f RealFileSystem) WriteFile(name string, data []byte, perms os.FileMode) error {
	err := os.WriteFile(name, data, perms)
	return err
}

// Fake file handlers (for testing)

// Name
func (f *FakeFile) Name() string {
	return "fakeFile.txt"
}

// Write
func (f *FakeFile) Write(b []byte) (n int, err error) {
	fmt.Printf("Written: %v", b)
	return 0, err
}

// Sync
func (f *FakeFile) Sync() error {
	return nil
}

// Close
func (f *FakeFile) Close() error {
	return nil
}

// Stat
func (f FakeFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Create
func (f FakeFileSystem) Create(_ string) (File, error) {
	return &FakeFile{}, nil
}

// Mkdir
func (f FakeFileSystem) Mkdir(_ string, _ os.FileMode) error {
	return nil
}

// MkdirTemp
func (f FakeFileSystem) MkdirTemp(name string, pattern string) (string, error) {
	// Set sensible defaults if a call is made using the usual "". "*" that usually
	// generates a random dir - see https://pkg.go.dev/os#MkdirTemp
	if name == "" {
		name = "dir1"
	}
	if pattern == "*" {
		pattern = "random"
	}
	dir := filepath.Join("tmp", name, pattern)
	return dir, nil
}

// MkdirAll
func (f FakeFileSystem) MkdirAll(_ string, _ os.FileMode) error {
	return nil
}

// Remove
func (f FakeFileSystem) Remove(_ string) error {
	return nil
}

// RemoveAll
func (f FakeFileSystem) RemoveAll(_ string) error {
	return nil
}

// Writefile
func (f FakeFileSystem) WriteFile(_ string, _ []byte, _ os.FileMode) error {
	return nil
}
