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

// package consoleprint contains the logic to update the console UI
package consoleprint

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// NodeCaptureStats represents stats for a node capture.
type NodeCaptureStats struct {
	startTime       int64
	endTime         int64
	status          string
	endProcessError string
	isCoordinator   bool
	progress        string
	protocol        string
	toolErrors      []string  // accumulated per-tool errors from UpdateNodeState calls
	jvmDeadline     time.Time // expected completion time for JVM tool countdown
}

// CollectionStats represents stats for a collection.
type CollectionStats struct {
	collectionMode    collects.CollectionMode      // shows the collectionMode used that sets defaults: light, standard, or healthcheck
	collectionArgs    string                       // collectionArgs arguments passed to ddc, useful for debugging
	ddcVersion        string                       // ddcVersion used during the collection
	logFile           string                       // logFile location of the ddc.log file
	TransfersComplete int                          // TransfersComplete shows the number of tarball transfers completed
	totalTransfers    int                          // totalTransfers shows the number of transfers of tarballs attempted
	totalCoordinators int                          // totalCoordinators is the total number of coordinators discovered
	totalExecutors    int                          // totalExecutors is the total number of executors discovered
	tarball           string                       // tarball is the location of the final tarball
	archiveBytesRead  int64                        // archiveBytesRead tracks bytes read during archive creation
	archiveTotalBytes int64                        // archiveTotalBytes is the total input size for archive progress
	nodeCaptureStats  map[string]*NodeCaptureStats // nodeCaptureStats is the map of nodes and their basic collection stats such as startTime, endTime and status
	result            string                       // result is the current result of the collection process
	startTime         int64                        // startTime in epoch seconds for the collection
	endTime           int64                        // endTime in epoch seconds for the collection
	activeThreads     int                          // activeThreads is the number of goroutines currently holding a semaphore slot
	maxThreads        int                          // maxThreads is the concurrency limit (semaphore capacity)
	queuedNodes       int                          // queuedNodes is the number of goroutines waiting to acquire a semaphore slot
	mu                sync.RWMutex                 // mu is the mutex to protect access to various fields (nodeCaptureStats, warnings, lastK8sFileCollected, etc)
}

var statusOut atomic.Bool

// spinnerFrames are braille characters for animated spinner display.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerIdx int

// TUI styles — allocated once to avoid re-creation every 200ms tick.
const panelWidth = 64

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("220")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 2).
			Align(lipgloss.Center).
			Width(panelWidth)
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	labelStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Width(16)
	okStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	warnStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	nodeNameStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	sectionTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)
)

// EnableStatusOutput enables the DDC json output
// that enables communication between DDC and the Dremio UI
func EnableStatusOutput() {
	statusOut.Store(true)
}

// DisableStatusOutput disables the DDC json output (used in tests).
func DisableStatusOutput() {
	statusOut.Store(false)
}

// IsStatusOutput reports whether JSON status output is enabled.
func IsStatusOutput() bool {
	return statusOut.Load()
}

// ErrorOut is for error messages so that the
// Dremio UI can display it.
type ErrorOut struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// WarnOut is for warning messages so that the
// Dremio UI can display it. They should
// be interesting but not fatal
type WarnOut struct {
	Type    string `json:"type"`
	Warning string `json:"warning"`
}

// WarningPrint will either output either in json or pure text
// depending on if statusOut is enabled or not
// these should be interesting but not fatal
func WarningPrint(msg string) {
	if statusOut.Load() {
		b, err := json.Marshal(WarnOut{Type: "warning", Warning: msg})
		if err != nil {
			// output a nested error if unable to marshal
			fmt.Printf("{\"warning\": \"%q\", \"nested\": \"%q\"}\n", err, msg)
			return
		}
		fmt.Println(string(b))
	} else {
		fmt.Println(msg)
	}
}

// ErrorPrint will either output either in json or pure text
// depending on if statusOut is enabled or not
func ErrorPrint(msg string) {
	if statusOut.Load() {
		b, err := json.Marshal(ErrorOut{Type: "error", Error: msg})
		if err != nil {
			fmt.Printf("{\"error\": \"%q\", \"nested\": \"%q\"}\n", err, msg)
			return
		}
		fmt.Println(string(b))
	} else {
		fmt.Println(msg)
	}
}

// SetVersion sets the DDC version for display. Call early before the ticker starts.
func SetVersion(version string) {
	c.mu.Lock()
	c.ddcVersion = version
	c.mu.Unlock()
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateRuntime(ddcVersion string, logFile string, coordinators, executors, extraTasks int) {
	c.mu.Lock()
	c.ddcVersion = ddcVersion
	c.logFile = logFile
	c.TransfersComplete = 0
	c.totalTransfers = coordinators + executors + 1 + extraTasks // +1 for K8s/cluster data collection
	c.totalCoordinators = coordinators
	c.totalExecutors = executors
	c.mu.Unlock()
}

func UpdateArchiveProgress(bytesRead, totalBytes int64) {
	c.mu.Lock()
	c.archiveBytesRead = bytesRead
	c.archiveTotalBytes = totalBytes
	c.mu.Unlock()
}

func UpdateTarballDir(tarballDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tarball = tarballDir

	// If the result already contains "COMPLETE", append the tarball information to it
	if c.result != "" && strings.Contains(c.result, "COMPLETE") && !strings.Contains(c.result, tarballDir) {
		c.result = fmt.Sprintf("%v\nTarball: %v", c.result, tarballDir)
	}
}

// StatusUpdate struct for json communication
// with the Dremio UI, this is how the Dremio UI
// picks up status changes for the collection
type StatusUpdate struct {
	Type   string `json:"type"`
	Result string `json:"result"` // Result shows the current result of the process, this can change over time
}

// UpdateResult either outputs a json text for
// the Dremio UI indicating the result status has been updated
// or just stores the result and updates the end time for processing later
func UpdateResult(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if statusOut.Load() {
		b, err := json.Marshal(StatusUpdate{Type: "result", Result: result})
		if err != nil {
			fmt.Printf("{\"error\": \"%q\", \"nested\": \"%q\"}\n", err, result)
			return
		}
		fmt.Println(string(b))
	}
	c.result = result
	c.endTime = time.Now().Unix()
}

// IncrementTransfersComplete increments the collection progress counter by one.
func IncrementTransfersComplete() {
	c.mu.Lock()
	c.TransfersComplete++
	c.mu.Unlock()
}

func UpdateCollectionArgs(collectionArgs string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectionArgs = collectionArgs
}

func UpdateCollectionMode(collectionMode collects.CollectionMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectionMode = collectionMode
}

// UpdateThreadStatus updates the thread concurrency counters displayed in the TUI.
// active is the number of goroutines currently holding a semaphore slot,
// max is the semaphore capacity, and queued is the number waiting to acquire.
func UpdateThreadStatus(active, max, queued int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeThreads = active
	c.maxThreads = max
	c.queuedNodes = queued
}

// c is the singleton that is the global collection
// stats that stores all the status updates used by
// the collection process.
var c *CollectionStats

func init() {
	initialize()
}

func initialize() {
	c = &CollectionStats{
		nodeCaptureStats: make(map[string]*NodeCaptureStats),
		startTime:        time.Now().Unix(),
	}
	if strings.Contains(os.Args[0], ".test") {
		isTerminal = false // disable ANSI escape codes in tests
	}
}

// Clear resets the UI entirely, this is really
// only useful for debugging and testing
func Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	initialize()
}

type NodeState struct {
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	StatusUX        string    `json:"status_ux"`
	Node            string    `json:"node"`
	Result          string    `json:"result"`
	EndProcess      bool      `json:"end_process"`
	EndProcessError string    `json:"-"` // Use json:"-" to exclude from JSON output
	IsCoordinator   bool      `json:"-"` // Use json:"-" to exclude from JSON output
	Progress        string    `json:"-"` // Use json:"-" to exclude from JSON output
	Protocol        string    `json:"-"` // Transport protocol: "WebSocket", "SPDY", "SSH", "Local"
	ToolErrors      []string  `json:"-"` // Per-tool errors accumulated across UpdateNodeState calls
	JVMDeadline     time.Time `json:"-"` // Expected completion time for JVM tool countdown display
}

// JSONSnapshot is the full status snapshot emitted each tick when --progress=json is active.
type JSONSnapshot struct {
	Type              string             `json:"type"`
	Version           string             `json:"version"`
	Mode              string             `json:"mode"`
	Coordinators      int                `json:"coordinators"`
	Executors         int                `json:"executors"`
	TransfersComplete int                `json:"transfers_complete"`
	TransfersTotal    int                `json:"transfers_total"`
	ThreadsActive     int                `json:"threads_active"`
	ThreadsMax        int                `json:"threads_max"`
	Queued            int                `json:"queued"`
	Result            string             `json:"result"`
	Tarball           string             `json:"tarball,omitempty"`
	ElapsedMs         int64              `json:"elapsed_ms"`
	Nodes             []JSONNodeSnapshot `json:"nodes"`
}

// JSONNodeSnapshot is the per-node status within a JSON snapshot.
type JSONNodeSnapshot struct {
	Node          string   `json:"node"`
	IsCoordinator bool     `json:"is_coordinator"`
	Status        string   `json:"status"`
	ElapsedMs     int64    `json:"elapsed_ms"`
	Failed        bool     `json:"failed"`
	ToolErrors    []string `json:"tool_errors,omitempty"`
}

const (
	ResultPending = "PENDING"
	ResultFailure = "FAILURE"
)

// this is the list of different collection steps that are also communicated
// back to the Dremio UI, changing this involves a code change in Dremio
// as well.
const (
	Starting                   = "STARTING"
	CreatingRemoteDir          = "CREATING_REMOTE_DIR"
	CopyDDCToHost              = "COPY_DDC_TO_HOST"
	SettingDDCPermissions      = "SETTING_DDC_PERMISSIONS"
	Collecting                 = "COLLECTING"
	CollectingAwaitingTransfer = "COLLECTING_AWAITING_TRANSFER"
	Streaming                  = "FETCHING"
	Completed                  = "COMPLETED"
	DiskUsage                  = "DISK_USAGE"
	DremioConfig               = "DREMIO_CONFIG"
	GcLog                      = "GC_LOG"
	Jfr                        = "JFR"
	Jstack                     = "JSTACK"
	JVMFlags                   = "JVM_FLAGS"
	MetadataLog                = "METADATA_LOG"
	OSConfig                   = "OS_CONFIG"
	Queries                    = "QUERIES"
	ReflectionLog              = "REFLECTION_LOG"
	ServerLog                  = "SERVER_LOG"
	Top                        = "TOP"
	AccelerationLog            = "ACCELERATION_LOG"
	AccessLog                  = "ACCESS_LOG"
	AuditLog                   = "AUDIT_LOG"
	JobProfiles                = "JOB_PROFILES"
	KVStore                    = "KV_STORE"
	SystemTable                = "SYSTEM_TABLE"
	Wlm                        = "WLM"
	HeapDump                   = "HEAP_DUMP"
)

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeState(nodeState NodeState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if statusOut.Load() {
		nodeState.Type = "node"
		b, err := json.Marshal(nodeState)
		if err != nil {
			fmt.Printf("{\"error\": \"%v\"}\n", strconv.Quote(err.Error()))
		} else {
			fmt.Println(string(b))
		}
	}
	node := nodeState.Node
	status := nodeState.StatusUX
	result := nodeState.Result
	if _, ok := c.nodeCaptureStats[node]; ok {
		// progress for job stats this is usually not set
		c.nodeCaptureStats[node].progress = nodeState.Progress
		if nodeState.Protocol != "" {
			c.nodeCaptureStats[node].protocol = nodeState.Protocol
		}
		// Accumulate per-tool errors across multiple UpdateNodeState calls.
		if len(nodeState.ToolErrors) > 0 {
			c.nodeCaptureStats[node].toolErrors = append(c.nodeCaptureStats[node].toolErrors, nodeState.ToolErrors...)
		}
		if !nodeState.JVMDeadline.IsZero() {
			c.nodeCaptureStats[node].jvmDeadline = nodeState.JVMDeadline
		}
		// failures should include a clear failure output in the status text
		if result == ResultFailure {

			status = status + " - " + ResultFailure
		}
		// set the status message on the node directly so we can display it when the
		// display is updated. Skip when StatusUX is empty to avoid overwriting a
		// previously set status (e.g. the final "Done:" from streaming).
		if status != "" {
			c.nodeCaptureStats[node].status = status
		}
		if nodeState.EndProcess {
			// set the end time and then increment the transfers complete counter
			// we only want to count it the first time, so we check to see if
			// the endTime has been set or not, if it has, then we do nothing.
			if c.nodeCaptureStats[node].endTime == 0 {
				if nodeState.Result != ResultFailure {
					c.TransfersComplete++
				}
				// Use accumulated tool errors as endProcessError if no explicit error was provided.
				endErr := nodeState.EndProcessError
				if endErr == "" && len(c.nodeCaptureStats[node].toolErrors) > 0 {
					endErr = strings.Join(c.nodeCaptureStats[node].toolErrors, "; ")
				}
				c.nodeCaptureStats[node].endProcessError = endErr
				c.nodeCaptureStats[node].endTime = time.Now().Unix()
			}
		}
	} else {
		// if the node is not present we initialize it and can
		// safely set the start time.
		c.nodeCaptureStats[node] = &NodeCaptureStats{
			startTime:     time.Now().Unix(),
			status:        status,
			isCoordinator: nodeState.IsCoordinator,
			protocol:      nodeState.Protocol,
			toolErrors:    nodeState.ToolErrors,
		}
	}
}

// EnterStatusScreen switches to the alternate screen buffer for flicker-free status rendering.
func EnterStatusScreen() {
	if isTerminal {
		fmt.Print(ansi.SetMode(ansi.ModeAltScreenSaveCursor))
	}
}

// ExitStatusScreen exits the alternate screen buffer and clears the restored
// normal-screen content so that the final PrintState render is the only output.
func ExitStatusScreen() {
	if isTerminal {
		fmt.Print(ansi.ResetMode(ansi.ModeAltScreenSaveCursor))
		// Clear any content left in the normal screen buffer (e.g. the TUI banner)
		// so the final status render doesn't appear below stale output.
		fmt.Print(ansi.CursorHomePosition + ansi.EraseScreenBelow)
	}
}

// isTerminal reports whether stdout is a terminal (not piped or redirected).
var isTerminal = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}()

// renderProgressBar renders a horizontal progress bar using block characters.
func renderProgressBar(current, total, width int) string {
	filledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	if total == 0 {
		return emptyStyle.Render(strings.Repeat("░", width))
	}
	filled := int(float64(current) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return filledStyle.Render(strings.Repeat("█", filled)) + emptyStyle.Render(strings.Repeat("░", width-filled))
}

// nodeSnap is a point-in-time copy of a node's capture stats, used for
// rendering outside the lock.
type nodeSnap struct {
	status        string
	progress      string
	startTime     int64
	endTime       int64
	isCoordinator bool
	endError      string
	protocol      string
	toolErrors    []string  // accumulated per-tool errors for error section rendering
	jvmDeadline   time.Time // expected JVM tool completion for countdown
}

// lastNonInteractiveResult tracks the last line printed in non-interactive mode
// to avoid duplicate output.
var lastNonInteractiveResult string

// PrintState renders the status display. In a terminal it overwrites in-place;
// when piped to another tool it falls back to simple line output.
func PrintState() {
	c.mu.Lock()

	total := c.totalTransfers
	transfersComplete := c.TransfersComplete
	collectionMode := c.collectionMode
	tarball := c.tarball
	startTime := c.startTime
	result := c.result
	activeThreads := c.activeThreads
	maxThreads := c.maxThreads
	queuedNodes := c.queuedNodes
	archiveBytesRead := c.archiveBytesRead
	archiveTotalBytes := c.archiveTotalBytes
	totalCoordinators := c.totalCoordinators
	totalExecutors := c.totalExecutors

	ddcVersion := "Unknown Version"
	if c.ddcVersion != "" {
		ddcVersion = c.ddcVersion
	}

	snaps := make(map[string]nodeSnap, len(c.nodeCaptureStats))
	for k, n := range c.nodeCaptureStats {
		var errs []string
		if len(n.toolErrors) > 0 {
			errs = make([]string, len(n.toolErrors))
			copy(errs, n.toolErrors)
		}
		snaps[k] = nodeSnap{
			status:        n.status,
			progress:      n.progress,
			startTime:     n.startTime,
			endTime:       n.endTime,
			isCoordinator: n.isCoordinator,
			endError:      n.endProcessError,
			protocol:      n.protocol,
			toolErrors:    errs,
			jvmDeadline:   n.jvmDeadline,
		}
	}

	c.mu.Unlock()

	// JSON mode: emit a full snapshot instead of TUI rendering.
	if statusOut.Load() {
		now := time.Now().Unix()
		nodes := make([]JSONNodeSnapshot, 0, len(snaps))
		for name, s := range snaps {
			var elapsedMs int64
			if s.startTime > 0 {
				end := s.endTime
				if end == 0 {
					end = now
				}
				elapsedMs = (end - s.startTime) * 1000
			}
			nodes = append(nodes, JSONNodeSnapshot{
				Node:          name,
				IsCoordinator: s.isCoordinator,
				Status:        s.status,
				ElapsedMs:     elapsedMs,
				Failed:        strings.Contains(s.status, ResultFailure),
				ToolErrors:    s.toolErrors,
			})
		}
		snap := JSONSnapshot{
			Type:              "status",
			Version:           ddcVersion,
			Mode:              string(collectionMode),
			Coordinators:      totalCoordinators,
			Executors:         totalExecutors,
			TransfersComplete: transfersComplete,
			TransfersTotal:    total,
			ThreadsActive:     activeThreads,
			ThreadsMax:        maxThreads,
			Queued:            queuedNodes,
			Result:            result,
			Tarball:           tarball,
			ElapsedMs:         (now - startTime) * 1000,
			Nodes:             nodes,
		}
		b, err := json.Marshal(snap)
		if err == nil {
			fmt.Println(string(b))
		}
		return
	}

	// Non-interactive fallback: when stdout is piped, print simple status lines.
	if !isTerminal {
		line := fmt.Sprintf("[%d/%d] %s", transfersComplete, total, result)
		if line != lastNonInteractiveResult {
			lastNonInteractiveResult = line
			fmt.Println(line)
		}
		return
	}

	// Categorize and sort node keys for stable output.
	var coordKeys, execKeys []string
	for k, s := range snaps {
		if s.isCoordinator {
			coordKeys = append(coordKeys, k)
		} else {
			execKeys = append(execKeys, k)
		}
	}
	sort.Slice(coordKeys, func(i, j int) bool {
		iMaster := strings.Contains(coordKeys[i], "master")
		jMaster := strings.Contains(coordKeys[j], "master")
		if iMaster != jMaster {
			return iMaster
		}
		return coordKeys[i] < coordKeys[j]
	})
	sort.Strings(execKeys)

	// Build result text.
	resultText := result
	if resultText == "" {
		resultText = fmt.Sprintf("STARTED AT %v", time.Unix(startTime, 0).Format(time.RFC1123))
	}
	if tarball != "" && !strings.Contains(resultText, tarball) && strings.HasPrefix(resultText, "COMPLETED AT") {
		resultText = fmt.Sprintf("%v - Tarball: %v", resultText, tarball)
	}

	// Collect failed nodes and nodes with tool errors for error reporting.
	var failedNodes []string
	var nodesWithToolErrors []string
	for name, s := range snaps {
		if strings.Contains(s.status, ResultFailure) {
			failedNodes = append(failedNodes, name)
		}
		if len(s.toolErrors) > 0 {
			nodesWithToolErrors = append(nodesWithToolErrors, name)
		}
	}
	sort.Strings(failedNodes)
	sort.Strings(nodesWithToolErrors)

	// Build error detail from failed nodes (full error) and nodes with tool errors.
	var errLogs strings.Builder
	rendered := make(map[string]bool)
	for _, name := range failedNodes {
		s := snaps[name]
		errLogs.WriteString(fmt.Sprintf("  Node: %v\n", name))
		if len(s.toolErrors) > 0 {
			for _, te := range s.toolErrors {
				errLogs.WriteString(fmt.Sprintf("    • %v\n", te))
			}
		} else if s.endError != "" {
			errLogs.WriteString(fmt.Sprintf("    • %v\n", s.endError))
		}
		rendered[name] = true
	}
	for _, name := range nodesWithToolErrors {
		if rendered[name] {
			continue
		}
		s := snaps[name]
		errLogs.WriteString(fmt.Sprintf("  Node: %v\n", name))
		for _, te := range s.toolErrors {
			errLogs.WriteString(fmt.Sprintf("    • %v\n", te))
		}
	}

	sep := dimStyle.Render(strings.Repeat("─", panelWidth+2)) // +2 for border chars to align with title box

	renderSection := func(title string) string {
		return sectionTitleStyle.Render(title) + "\n" + sep
	}

	row := func(label, value string) string {
		return fmt.Sprintf("  %s  %s\n", labelStyle.Render(label), value)
	}

	// --- Build output ---
	var sb strings.Builder

	// Move cursor to top-left and clear to end of screen.
	// In alt screen mode this gives flicker-free full redraws.
	sb.WriteString(ansi.CursorHomePosition)
	sb.WriteString(ansi.EraseScreenBelow)
	// Strip "ddc " prefix from version if present, so title shows clean version.
	titleVersion := strings.TrimPrefix(ddcVersion, "ddc ")
	sb.WriteString(titleStyle.Render("Dremio Diagnostic Collector - " + titleVersion))
	sb.WriteString("\n")

	// Timestamp / mode info line (version is already in the header).
	tsStr := dimStyle.Render(time.Now().Format("Mon, 02 Jan 2006 15:04:05 MST"))
	pipeSep := dimStyle.Render("  │  ")
	infoLine := tsStr
	if collectionMode != "" {
		modeStr := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render(strings.ToUpper(string(collectionMode)))
		infoLine += pipeSep + modeStr
	}
	sb.WriteString("  " + infoLine + "\n\n")

	// Status section.
	sb.WriteString("  " + renderSection("status") + "\n\n")

	// Collection progress row with progress bar.
	bar := renderProgressBar(transfersComplete, total, 12)
	sb.WriteString(row("Collection", fmt.Sprintf("%s  %s", bar, dimStyle.Render(fmt.Sprintf("%d/%d", transfersComplete, total)))))

	// Node count rows.
	coordCountStr := warnStyle.Render(strconv.Itoa(c.totalCoordinators))
	if c.totalCoordinators == 0 {
		coordCountStr = dimStyle.Render("0")
	}
	sb.WriteString(row("Coordinators", coordCountStr))

	execCountStr := warnStyle.Render(strconv.Itoa(c.totalExecutors))
	if c.totalExecutors == 0 {
		execCountStr = dimStyle.Render("0")
	}
	sb.WriteString(row("Executors", execCountStr))

	// Thread concurrency rows — only shown when maxThreads is set (collection is active).
	if maxThreads > 0 {
		var threadStr, queuedStr string
		if activeThreads < 0 {
			// JVM/heap dump phase — semaphore not used.
			threadStr = dimStyle.Render("N/A")
			queuedStr = dimStyle.Render("N/A")
		} else {
			threadStr = fmt.Sprintf("%d/%d", activeThreads, maxThreads)
			if activeThreads > 0 {
				threadStr = warnStyle.Render(threadStr)
			} else {
				threadStr = dimStyle.Render(threadStr)
			}
			queuedStr = strconv.Itoa(queuedNodes)
			if queuedNodes > 0 {
				queuedStr = warnStyle.Render(queuedStr)
			} else {
				queuedStr = dimStyle.Render(queuedStr)
			}
		}
		sb.WriteString(row("Threads", threadStr))
		sb.WriteString(row("Queued", queuedStr))
	}

	// Failed nodes row — placed after Queued so active/queued work is visible first.
	var failedRendered string
	if len(failedNodes) == 0 {
		failedRendered = okStyle.Render("None")
	} else {
		failedRendered = errStyle.Render(strings.Join(failedNodes, ", "))
	}
	sb.WriteString(row("Failed Nodes", failedRendered))

	// Status activity line — full width, no label prefix.
	var resultRendered string
	resultUpper := strings.ToUpper(resultText)
	switch {
	case strings.Contains(resultUpper, "FAIL"), strings.Contains(resultUpper, "CANCEL"):
		resultRendered = errStyle.Render("✗ " + resultText)
	case strings.Contains(resultUpper, "COMPLETE"):
		resultRendered = okStyle.Render("✓") + " " + nodeNameStyle.Render(resultText)
	default:
		spin := warnStyle.Render(spinnerFrames[spinnerIdx%len(spinnerFrames)])
		resultRendered = spin + " " + warnStyle.Render(resultText)
		if archiveTotalBytes > 0 {
			pct := int(float64(archiveBytesRead) / float64(archiveTotalBytes) * 100)
			if pct > 100 {
				pct = 100
			}
			archiveBar := renderProgressBar(int(archiveBytesRead), int(archiveTotalBytes), 12)
			resultRendered += "  " + archiveBar + " " + dimStyle.Render(fmt.Sprintf("%d%%", pct))
		}
	}
	sb.WriteString("\n  " + resultRendered + "\n\n")

	// Node detail sections.
	renderNodes := func(header string, keys []string) {
		if len(keys) == 0 {
			return
		}
		sb.WriteString("  " + renderSection(header) + "\n\n")
		for i, key := range keys {
			s := snaps[key]

			status := s.status
			if s.progress != "" {
				status = fmt.Sprintf("%v — %v", status, s.progress)
			}

			var dot string
			switch {
			case strings.Contains(s.status, ResultFailure):
				dot = errStyle.Render("✗")
			case s.endTime > 0:
				dot = okStyle.Render("✓")
			default:
				dot = warnStyle.Render(spinnerFrames[spinnerIdx%len(spinnerFrames)])
			}

			protocolTag := ""
			if s.protocol != "" {
				protocolTag = " " + dimStyle.Render("["+s.protocol+"]")
			}
			sb.WriteString(fmt.Sprintf("  %s %d. %s%s\n", dot, i+1, nodeNameStyle.Render(key), protocolTag))

			statusLine := "       " + dimStyle.Render(status)
			if strings.HasPrefix(status, "Running:") && !s.jvmDeadline.IsZero() {
				if strings.Contains(status, "heap-dump") {
					statusLine += dimStyle.Render("  [dumping...]")
				} else {
					remaining := int(time.Until(s.jvmDeadline).Seconds())
					if remaining <= 0 {
						statusLine += dimStyle.Render("  [finalizing...]")
					} else {
						statusLine += dimStyle.Render(fmt.Sprintf("  [%ds remaining]", remaining))
					}
				}
			} else if s.endTime > 0 {
				elapsed := int(s.endTime) - int(s.startTime)
				statusLine += dimStyle.Render(fmt.Sprintf("  (%ds)", elapsed))
			}
			sb.WriteString(statusLine + "\n")
		}
		sb.WriteString("\n")
	}

	renderNodes("coordinator nodes", coordKeys)
	renderNodes("executor nodes", execKeys)

	// Error detail for failed nodes.
	if errLogs.Len() > 0 {
		sb.WriteString("  " + renderSection("errors") + "\n\n")
		sb.WriteString(errStyle.Render(errLogs.String()))
		sb.WriteString("\n")
	}

	output := sb.String()
	_, err := fmt.Print(output)
	if err != nil {
		fmt.Printf("unable to write output: (%v)\n", err)
	}

	// Advance spinner for next render.
	spinnerIdx++
}
