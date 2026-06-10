// Copyright 2023 Dremio Corporation
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

// Package configui provides charmbracelet/huh-based interactive configuration screens for DDC v4.
package configui

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
)

// bannerStyle matches the status screen's rounded-border title box.
var bannerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("220")).
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("214")).
	Padding(0, 2).
	Align(lipgloss.Center).
	Width(52)

// PrintBanner prints the DDC header. If newerVersion is non-empty, an update
// notice is appended below the title inside the same box.
func PrintBanner(version, newerVersion string) {
	title := fmt.Sprintf("Dremio Diagnostic Collector - %s", version)
	if newerVersion != "" {
		title += fmt.Sprintf("\n  Update available: %s", newerVersion)
	}
	fmt.Println()
	fmt.Println(bannerStyle.Render(title))
	fmt.Println()
}

// StandardConfig holds the user's choices from the standard mode config screen.
type StandardConfig struct {
	// Transport info (set by caller, not shown in form — used for CLI command generation).
	Transport   string // "ssh", "k8s", "local", or "local-k8s"
	Namespace   string
	Kubeconfig  string // empty unless user supplied a non-default kubeconfig path
	Coordinator string
	Executors   string
	SSHUser     string
	K8sContext  string
	DremioHome  string

	CoordinatorLogDir string
	ExecutorLogDir    string
	DremioConfDir     string
	DremioRocksDBDir  string
	AllowInsecureSSL  bool

	CollectServerLogs  bool
	ServerLogsDays     int
	CollectTrackerJSON bool
	TrackerJSONDays    int
	CollectVacuumLog   bool
	VacuumLogDays      int
	CollectMetaRefresh bool
	CollectQueriesJSON bool
	QueriesJSONDays    int
	CollectQueriesPerf bool
	QueriesPerfDays    int

	CollectWLM           bool
	CollectSystemTables  bool
	SystemTables         []string
	CollectContainerLogs bool

	Cancelled        bool
	ShowCLICmd       bool
	GeneratedCommand string
}

// DiagnosisConfig holds the user's choices from the diagnosis mode config screen.
type DiagnosisConfig struct {
	// Transport info (set by caller, not shown in form — used for CLI command generation).
	Transport   string // "ssh", "k8s", "local", or "local-k8s"
	Namespace   string
	Kubeconfig  string // empty unless user supplied a non-default kubeconfig path
	Coordinator string
	Executors   string
	SSHUser     string
	K8sContext  string
	DremioHome  string

	CoordinatorLogDir string
	ExecutorLogDir    string
	DremioConfDir     string
	DremioRocksDBDir  string
	AllowInsecureSSL  bool

	Days      int
	DateStart string

	CollectJFR           bool
	CollectJStack        bool
	CollectTop           bool
	CollectAsyncProfiler bool
	CollectHeapDump      bool
	DiagTimeSeconds      int

	CollectServerLogs     bool
	CollectGCLogs         bool
	CollectTrackerJSON    bool
	CollectVacuumLog      bool
	CollectAcceleration   bool
	CollectAccess         bool
	CollectHSErr          bool
	CollectHiveDeprecated bool
	CollectMetaRefresh    bool
	CollectQueriesJSON    bool
	CollectQueriesPerf    bool

	DremioEndpoint             string
	PATToken                   string
	CollectWLM                 bool
	CollectKVStore             bool
	CollectProblematicProfiles bool
	CollectSystemTables        bool
	SystemTables               []string
	CollectContainerLogs       bool

	SelectedCoordinators []string
	SelectedExecutors    []string

	Cancelled        bool
	ShowCLICmd       bool
	GeneratedCommand string
}

// SortNodesMasterFirst sorts node names into three tiers:
// (1) names containing "master", (2) names containing "coordinator",
// (3) everything else (executors). Within each tier, names are sorted
// alphabetically. Returns a new slice — the input is not modified.
func SortNodesMasterFirst(nodes []string) []string {
	if len(nodes) == 0 {
		return nil
	}
	var masters, coordinators, others []string
	for _, n := range nodes {
		lower := strings.ToLower(n)
		switch {
		case strings.Contains(lower, "master"):
			masters = append(masters, n)
		case strings.Contains(lower, "coordinator"):
			coordinators = append(coordinators, n)
		default:
			others = append(others, n)
		}
	}
	sort.Strings(masters)
	sort.Strings(coordinators)
	sort.Strings(others)
	out := make([]string, 0, len(nodes))
	out = append(out, masters...)
	out = append(out, coordinators...)
	out = append(out, others...)
	return out
}

// intStr adapts an int field for huh which binds to *string.
type intStr struct {
	str string
	dst *int
}

func newIntStr(val int, dst *int) *intStr {
	return &intStr{str: strconv.Itoa(val), dst: dst}
}

func (s *intStr) sync() {
	if v, err := strconv.Atoi(s.str); err == nil {
		*s.dst = v
	}
}

func validateInt(s string) error {
	if s == "" {
		return fmt.Errorf("required")
	}
	if _, err := strconv.Atoi(s); err != nil {
		return fmt.Errorf("must be a number")
	}
	return nil
}

func validateDateOnly(s string) error {
	if s == "" {
		return nil // empty = auto from days
	}
	_, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fmt.Errorf("use date format: 2006-01-02")
	}
	return nil
}

// DetectedPaths holds paths discovered from target nodes via the --discover flag.
// Empty strings mean detection failed for that path.
type DetectedPaths struct {
	CoordinatorLogDir string
	ExecutorLogDir    string
	ConfDir           string
	RocksDBDir        string
	Endpoint          string // e.g. "http://192.168.8.30:9047"
	Detected          bool   // true if discovery ran successfully
	Hostname          string // coordinator node that was probed
	ExecutorHostname  string // executor node that was probed

	// Transport info — set by caller for CLI command generation.
	Transport   string // "ssh", "k8s", "local", or "local-k8s"
	Namespace   string
	Kubeconfig  string // empty unless user supplied a non-default kubeconfig path
	Coordinator string
	Executors   string
	SSHUser     string
	K8sContext  string
	DremioHome  string
}

// RunStandardConfigScreen displays the interactive standard mode config form.
// If paths were detected from a target node, they are used as defaults.
func RunStandardConfigScreen(detected *DetectedPaths, _ string) (*StandardConfig, error) {
	stdDef := conf.StandardDefaultMap()
	coordinatorLogDir := conf.GetStringDefault(stdDef, conf.KeyDremioLogDir)
	executorLogDir := conf.GetStringDefault(stdDef, conf.KeyDremioLogDir)
	confDir := conf.GetStringDefault(stdDef, conf.KeyDremioConfDir)
	rocksDir := conf.GetStringDefault(stdDef, conf.KeyDremioRocksdbDir)
	coordinatorLogDesc := "(default — override if needed)"
	executorLogDesc := "(default — override if needed)"
	confDesc := "(default — override if needed)"
	if detected != nil && detected.Detected {
		if detected.CoordinatorLogDir != "" {
			coordinatorLogDir = detected.CoordinatorLogDir
			coordinatorLogDesc = fmt.Sprintf("(autodetected from %s)", detected.Hostname)
		}
		if detected.ExecutorLogDir != "" {
			executorLogDir = detected.ExecutorLogDir
			if detected.ExecutorHostname != "" {
				executorLogDesc = fmt.Sprintf("(autodetected from %s)", detected.ExecutorHostname)
			}
		}
		if detected.ConfDir != "" {
			confDir = detected.ConfDir
			confDesc = fmt.Sprintf("(autodetected from %s)", detected.Hostname)
		}
		if detected.RocksDBDir != "" {
			rocksDir = detected.RocksDBDir
		}
	}

	cfg := &StandardConfig{
		CoordinatorLogDir:  coordinatorLogDir,
		ExecutorLogDir:     executorLogDir,
		DremioConfDir:      confDir,
		DremioRocksDBDir:   rocksDir,
		AllowInsecureSSL:   conf.GetBoolDefault(stdDef, conf.KeyAllowInsecureSSL),
		CollectServerLogs:  conf.GetBoolDefault(stdDef, conf.KeyCollectServerLogs),
		ServerLogsDays:     conf.GetIntDefault(stdDef, conf.KeyServerLogsNumDays),
		CollectTrackerJSON: conf.GetBoolDefault(stdDef, conf.KeyCollectTrackerJSON),
		TrackerJSONDays:    conf.GetIntDefault(stdDef, conf.KeyTrackerJSONNumDays),
		CollectVacuumLog:   conf.GetBoolDefault(stdDef, conf.KeyCollectVacuumLog),
		VacuumLogDays:      conf.GetIntDefault(stdDef, conf.KeyVacuumLogNumDays),
		CollectMetaRefresh: conf.GetBoolDefault(stdDef, conf.KeyCollectMetaRefreshLog),
		CollectQueriesJSON: conf.GetBoolDefault(stdDef, conf.KeyCollectQueriesJSON),
		QueriesJSONDays:    conf.GetIntDefault(stdDef, conf.KeyQueriesJSONNumDays),
		CollectQueriesPerf: conf.GetBoolDefault(stdDef, conf.KeyCollectQueriesPerfJSON),
		QueriesPerfDays:    conf.GetIntDefault(stdDef, conf.KeyQueriesPerfNumDays),
		CollectWLM:         conf.GetBoolDefault(stdDef, conf.KeyCollectWLM),
		Cancelled:          true,
	}
	// Set transport info for CLI command generation.
	if detected != nil {
		cfg.Transport = detected.Transport
		cfg.Namespace = detected.Namespace
		cfg.Kubeconfig = detected.Kubeconfig
		cfg.Coordinator = detected.Coordinator
		cfg.Executors = detected.Executors
		cfg.SSHUser = detected.SSHUser
		cfg.K8sContext = detected.K8sContext
		cfg.DremioHome = detected.DremioHome
	}

	// Log collection uses select-based day choices (0 = skip)
	var queriesDayChoice, queriesPerfDayChoice, serverDayChoice, trackerDayChoice, vacuumDayChoice int

	// defaults from conf
	queriesDayChoice = cfg.QueriesJSONDays
	queriesPerfDayChoice = cfg.QueriesPerfDays
	serverDayChoice = cfg.ServerLogsDays
	trackerDayChoice = cfg.TrackerJSONDays
	vacuumDayChoice = cfg.VacuumLogDays

	proceed := true

	// Build path fields — local transport shows a single "Log dir"; SSH/K8s show separate coordinator/executor fields
	var pathFields []huh.Field
	if cfg.Transport == "local" {
		pathFields = append(pathFields, huh.NewInput().Title("Log dir").Value(&cfg.CoordinatorLogDir).Description(coordinatorLogDesc))
	} else {
		pathFields = append(pathFields,
			huh.NewInput().Title("Coordinator log dir").Value(&cfg.CoordinatorLogDir).Description(coordinatorLogDesc),
			huh.NewInput().Title("Executor log dir").Value(&cfg.ExecutorLogDir).Description(executorLogDesc),
		)
	}
	pathFields = append(pathFields, huh.NewInput().Title("Dremio conf dir").Value(&cfg.DremioConfDir).Description(confDesc))

	form := huh.NewForm(
		huh.NewGroup(pathFields...).Title("Paths").Description(" "),

		buildStandardLogGroup(cfg, &queriesDayChoice, &queriesPerfDayChoice, &serverDayChoice, &trackerDayChoice, &vacuumDayChoice),

		huh.NewGroup(
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
		).Title("Additional collections").Description(" "),

		huh.NewGroup(
			huh.NewNote().Title("CLI command").DescriptionFunc(func() string {
				return escapeForHuh(buildStandardCLICommand(cfg, queriesDayChoice, queriesPerfDayChoice, serverDayChoice, trackerDayChoice, vacuumDayChoice))
			}, cfg),
			huh.NewConfirm().Title("Proceed with collection?").Value(&proceed).Inline(true),
		),
	).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	// Apply log day selections (0 = skip)
	cfg.CollectQueriesJSON = queriesDayChoice > 0
	cfg.QueriesJSONDays = queriesDayChoice
	cfg.CollectQueriesPerf = queriesPerfDayChoice > 0
	cfg.QueriesPerfDays = queriesPerfDayChoice
	cfg.CollectServerLogs = serverDayChoice > 0
	cfg.ServerLogsDays = serverDayChoice
	cfg.CollectTrackerJSON = trackerDayChoice > 0
	cfg.TrackerJSONDays = trackerDayChoice
	cfg.CollectVacuumLog = vacuumDayChoice > 0
	cfg.VacuumLogDays = vacuumDayChoice

	cfg.Cancelled = !proceed
	cfg.GeneratedCommand = buildStandardCLICommand(cfg, queriesDayChoice, queriesPerfDayChoice, serverDayChoice, trackerDayChoice, vacuumDayChoice)

	return cfg, nil
}

// RunDiagnosisConfigScreen displays the interactive diagnosis mode config form.
// If paths were detected from a target node, they are used as defaults.
// discoveredCoordinators and discoveredExecutors are the node names found by
// discovery — they populate the Node Selection multi-select page.
func RunDiagnosisConfigScreen(detected *DetectedPaths, version string, discoveredCoordinators, discoveredExecutors []string) (*DiagnosisConfig, error) {
	diagDef := conf.DiagnosisDefaultMap()
	coordinatorLogDir := conf.GetStringDefault(diagDef, conf.KeyDremioLogDir)
	executorLogDir := conf.GetStringDefault(diagDef, conf.KeyDremioLogDir)
	confDir := conf.GetStringDefault(diagDef, conf.KeyDremioConfDir)
	rocksDir := conf.GetStringDefault(diagDef, conf.KeyDremioRocksdbDir)
	coordinatorLogDesc := "(default — override if needed)"
	executorLogDesc := "(default — override if needed)"
	confDesc := "(default — override if needed)"
	endpoint := conf.GetStringDefault(diagDef, conf.KeyDremioEndpoint)
	endpointDesc := "(default)"
	if detected != nil && detected.Detected {
		if detected.CoordinatorLogDir != "" {
			coordinatorLogDir = detected.CoordinatorLogDir
			coordinatorLogDesc = fmt.Sprintf("(autodetected from %s)", detected.Hostname)
		}
		if detected.ExecutorLogDir != "" {
			executorLogDir = detected.ExecutorLogDir
			if detected.ExecutorHostname != "" {
				executorLogDesc = fmt.Sprintf("(autodetected from %s)", detected.ExecutorHostname)
			}
		}
		if detected.ConfDir != "" {
			confDir = detected.ConfDir
			confDesc = fmt.Sprintf("(autodetected from %s)", detected.Hostname)
		}
		if detected.RocksDBDir != "" {
			rocksDir = detected.RocksDBDir
		}
		if detected.Endpoint != "" {
			endpoint = detected.Endpoint
			endpointDesc = fmt.Sprintf("(autodetected from %s)", detected.Hostname)
		}
	}

	cfg := &DiagnosisConfig{
		CoordinatorLogDir:          coordinatorLogDir,
		ExecutorLogDir:             executorLogDir,
		DremioConfDir:              confDir,
		DremioRocksDBDir:           rocksDir,
		AllowInsecureSSL:           conf.GetBoolDefault(diagDef, conf.KeyAllowInsecureSSL),
		Days:                       conf.GetIntDefault(diagDef, conf.KeyDremioLogsNumDays),
		CollectJFR:                 conf.GetBoolDefault(diagDef, conf.KeyCollectJFR),
		CollectJStack:              conf.GetBoolDefault(diagDef, conf.KeyCollectJStack),
		CollectTop:                 conf.GetBoolDefault(diagDef, conf.KeyCollectTop),
		CollectAsyncProfiler:       conf.GetBoolDefault(diagDef, conf.KeyCollectAsyncProfiler),
		CollectHeapDump:            conf.GetBoolDefault(diagDef, conf.KeyCaptureHeapDump),
		DiagTimeSeconds:            conf.GetIntDefault(diagDef, conf.KeyDiagTimeSeconds),
		CollectServerLogs:          conf.GetBoolDefault(diagDef, conf.KeyCollectServerLogs),
		CollectGCLogs:              conf.GetBoolDefault(diagDef, conf.KeyCollectGCLogs),
		CollectTrackerJSON:         conf.GetBoolDefault(diagDef, conf.KeyCollectTrackerJSON),
		CollectVacuumLog:           conf.GetBoolDefault(diagDef, conf.KeyCollectVacuumLog),
		CollectAcceleration:        conf.GetBoolDefault(diagDef, conf.KeyCollectAccelerationLog),
		CollectAccess:              conf.GetBoolDefault(diagDef, conf.KeyCollectAccessLog),
		CollectHSErr:               conf.GetBoolDefault(diagDef, conf.KeyCollectHSErrFiles),
		CollectHiveDeprecated:      conf.GetBoolDefault(diagDef, conf.KeyCollectHiveDeprecatedLog),
		CollectMetaRefresh:         conf.GetBoolDefault(diagDef, conf.KeyCollectMetaRefreshLog),
		CollectQueriesJSON:         conf.GetBoolDefault(diagDef, conf.KeyCollectQueriesJSON),
		CollectQueriesPerf:         conf.GetBoolDefault(diagDef, conf.KeyCollectQueriesPerfJSON),
		CollectContainerLogs:       true, // K8s diagnosis always collects container logs by default
		DremioEndpoint:             endpoint,
		CollectWLM:                 conf.GetBoolDefault(diagDef, conf.KeyCollectWLM),
		CollectKVStore:             conf.GetBoolDefault(diagDef, conf.KeyCollectKVStoreReport),
		CollectProblematicProfiles: conf.GetBoolDefault(diagDef, conf.KeyCollectProblematicProfiles),
		CollectSystemTables:        conf.GetBoolDefault(diagDef, conf.KeyCollectSystemTablesExport),
		Cancelled:                  true,
	}
	// Set transport info for CLI command generation.
	if detected != nil {
		cfg.Transport = detected.Transport
		cfg.Namespace = detected.Namespace
		cfg.Kubeconfig = detected.Kubeconfig
		cfg.Coordinator = detected.Coordinator
		cfg.Executors = detected.Executors
		cfg.SSHUser = detected.SSHUser
		cfg.K8sContext = detected.K8sContext
		cfg.DremioHome = detected.DremioHome
	}

	days := newIntStr(cfg.Days, &cfg.Days)
	diagDur := newIntStr(cfg.DiagTimeSeconds, &cfg.DiagTimeSeconds)
	// Start date override — empty means "use Days to collect" above.
	dateStart := ""
	proceed := true
	var diagLogTypes []string
	var selectedTools []string

	// Build combined node list sorted master-first for the Node Selection page.
	allNodes := make([]string, 0, len(discoveredCoordinators)+len(discoveredExecutors))
	allNodes = append(allNodes, discoveredCoordinators...)
	allNodes = append(allNodes, discoveredExecutors...)
	allNodes = SortNodesMasterFirst(allNodes)

	var nodeOptions []huh.Option[string]
	for _, n := range allNodes {
		nodeOptions = append(nodeOptions, huh.NewOption(n, n).Selected(true))
	}
	var selectedNodes []string

	// Build path fields — local transport shows a single "Log dir"; SSH/K8s show separate coordinator/executor fields
	var diagPathFields []huh.Field
	if cfg.Transport == "local" {
		diagPathFields = append(diagPathFields, huh.NewInput().Title("Log dir").Value(&cfg.CoordinatorLogDir).Description(coordinatorLogDesc))
	} else {
		diagPathFields = append(diagPathFields,
			huh.NewInput().Title("Coordinator log dir").Value(&cfg.CoordinatorLogDir).Description(coordinatorLogDesc),
			huh.NewInput().Title("Executor log dir").Value(&cfg.ExecutorLogDir).Description(executorLogDesc),
		)
	}
	diagPathFields = append(diagPathFields, huh.NewInput().Title("Dremio conf dir").Value(&cfg.DremioConfDir).Description(confDesc))

	// Build "Logs Collection" fields — date range + log types + toggles moved here
	logsCollectionFields := []huh.Field{
		huh.NewInput().Title("Days to collect").Value(&days.str).CharLimit(4).Validate(validateInt),
		huh.NewInput().Title("Start Date").Value(&dateStart).
			PlaceholderFunc(func() string {
				if n, err := strconv.Atoi(days.str); err == nil && n > 0 {
					return time.Now().AddDate(0, 0, -n).Format("2006-01-02") + " [auto]"
				}
				return ""
			}, &days.str).
			Validate(validateDateOnly),
	}

	// Single unified multi-select for all log types and data collections
	logsAndDataOptions := []huh.Option[string]{
		huh.NewOption("queries.json", "queries").Selected(cfg.CollectQueriesJSON),
		huh.NewOption("Queries Performance Data", "queries-perf").Selected(cfg.CollectQueriesPerf),
		huh.NewOption("Server logs", "server").Selected(cfg.CollectServerLogs),
		huh.NewOption("GC logs", "gc").Selected(cfg.CollectGCLogs),
		huh.NewOption("hs_err crash dumps", "hserr").Selected(cfg.CollectHSErr),
	}
	if cfg.Transport == "k8s" {
		logsAndDataOptions = append(logsAndDataOptions,
			huh.NewOption("K8s container logs", "container-logs").Selected(cfg.CollectContainerLogs),
		)
	}
	logsAndDataOptions = append(logsAndDataOptions,
		huh.NewOption("Tracker JSON", "tracker").Selected(cfg.CollectTrackerJSON),
		huh.NewOption("Vacuum log", "vacuum").Selected(cfg.CollectVacuumLog),
		huh.NewOption("Hive deprecated log", "hive").Selected(cfg.CollectHiveDeprecated),
		huh.NewOption("Acceleration log", "acceleration").Selected(cfg.CollectAcceleration),
		huh.NewOption("Access log", "access").Selected(cfg.CollectAccess),
		huh.NewOption("Metadata refresh log", "meta-refresh").Selected(cfg.CollectMetaRefresh),
		huh.NewOption("Problematic job profiles [Requires PAT token]", "profiles").Selected(cfg.CollectProblematicProfiles),
		huh.NewOption("KV store report [Requires PAT token]", "kvstore").Selected(cfg.CollectKVStore),
	)
	logsCollectionFields = append(logsCollectionFields,
		huh.NewMultiSelect[string]().
			Title("Logs and data to collect").
			Options(logsAndDataOptions...).
			Value(&diagLogTypes),
	)

	form := huh.NewForm(
		// Group 1: Paths
		huh.NewGroup(diagPathFields...).Title("Paths").Description(" "),

		// Group 2: Node Selection — hidden when no nodes discovered or SSH/local transport
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Nodes to collect from (deselect to skip)").
				Options(nodeOptions...).
				Value(&selectedNodes),
		).Title("Node selection").Description(" ").WithHideFunc(func() bool { return len(allNodes) == 0 || cfg.Transport == "ssh" || cfg.Transport == "local" }),

		// Group 3: Logs Collection (date range + log types + additional collections)
		huh.NewGroup(logsCollectionFields...).Title("Logs Collection").Description(" "),

		// Group 4: API-based collection — conditional: only shown when profiles or KV store selected
		huh.NewGroup(
			huh.NewInput().Title("Dremio endpoint").Value(&cfg.DremioEndpoint).Description(endpointDesc),
			huh.NewConfirm().Title("Allow insecure SSL").Value(&cfg.AllowInsecureSSL).Inline(true),
			huh.NewInput().Title("PAT token").Value(&cfg.PATToken).EchoMode(huh.EchoModePassword).
				Description("Required for job profiles and KV store collection").
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("PAT token is required when collecting job profiles or KV store")
					}
					result := ValidatePAT(cfg.DremioEndpoint, s, cfg.AllowInsecureSSL)
					if strings.Contains(result, "failed") {
						return fmt.Errorf("%s", result)
					}
					return nil
				}),
		).Title("API-based collection (targets dremio-master-0)").Description(" ").
			WithHideFunc(func() bool {
				for _, v := range diagLogTypes {
					if v == "profiles" || v == "kvstore" {
						return false
					}
				}
				return true
			}),

		// Group 5: Dremio System Tables & WLM (always shown)
		huh.NewGroup(
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
		).Title("Dremio System Tables & WLM").Description(" "),

		// Group 6: Diagnostics tools (JVM tools + heap dump only)
		buildDiagnosticsToolsGroup(&selectedTools, diagDur, cfg),

		// Group 7: CLI Command/Proceed
		huh.NewGroup(
			huh.NewNote().Title("CLI command").DescriptionFunc(func() string {
				// Sync multi-selects to cfg so the CLI command reflects current selections.
				syncDiagLogTypes(cfg, diagLogTypes)
				return escapeForHuh(buildDiagnosisCLICommand(cfg, &selectedTools, &selectedNodes, &days.str, &diagDur.str, &dateStart))
			}, cfg),
			huh.NewConfirm().Title("Proceed with collection?").Value(&proceed).Inline(true),
		),
	).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	days.sync()
	diagDur.sync()

	// Post-form date resolution: start date is optional, days is always set.
	if dateStart != "" {
		cfg.DateStart = dateStart
	}

	// Map multi-select log types back to config booleans
	syncDiagLogTypes(cfg, diagLogTypes)

	// Map multi-select tool choices back to config booleans
	mapSelectedTools(selectedTools, cfg)

	// Map selected nodes back to coordinator/executor lists by intersecting
	// with the original discovered lists.
	selectedSet := make(map[string]bool, len(selectedNodes))
	for _, n := range selectedNodes {
		selectedSet[n] = true
	}
	for _, c := range discoveredCoordinators {
		if selectedSet[c] {
			cfg.SelectedCoordinators = append(cfg.SelectedCoordinators, c)
		}
	}
	for _, e := range discoveredExecutors {
		if selectedSet[e] {
			cfg.SelectedExecutors = append(cfg.SelectedExecutors, e)
		}
	}

	cfg.Cancelled = !proceed
	cfg.GeneratedCommand = buildDiagnosisCLICommand(cfg, &selectedTools, &selectedNodes, &days.str, &diagDur.str, &dateStart)

	return cfg, nil
}

// buildStandardLogGroup builds the "Log collection" group, including the container logs
// confirm only when in K8s mode (namespace is set).
// allSystemTables lists every available system table with display names.
// The selected state is derived from conf.SystemTableList() defaults.
var allSystemTables = []struct {
	display string
	value   string
}{
	{"sys.version", "version"},
	{"sys.options", "options"},
	{"sys.roles", "roles"},
	{"sys.membership", "membership"},
	{"sys.privileges", "privileges"},
	{"sys.reflections", "reflections"},
	{"sys.materializations", "materializations"},
	{"sys.refreshes", "refreshes"},
	{"sys.reflection_dependencies", "reflection_dependencies"},
	{"sys.tables (expensive)", "tables"},
	{"sys.views (expensive)", "views"},
	{"sys.jobs_recent (expensive)", "jobs_recent"},
}

// buildSystemTablesMultiSelect returns the system tables multi-select field used by both
// the standard and diagnosis config screens. Selected state comes from conf.SystemTableList().
func buildSystemTablesMultiSelect(value *[]string) *huh.MultiSelect[string] {
	defaults := make(map[string]bool, len(conf.SystemTableList()))
	for _, t := range conf.SystemTableList() {
		defaults[t] = true
	}
	var opts []huh.Option[string]
	for _, t := range allSystemTables {
		opts = append(opts, huh.NewOption(t.display, t.value).Selected(defaults[t.value]))
	}
	return huh.NewMultiSelect[string]().
		Title("System tables to collect").
		Options(opts...).
		Value(value)
}

func buildStandardLogGroup(cfg *StandardConfig, queriesDayChoice, queriesPerfDayChoice, serverDayChoice, trackerDayChoice, vacuumDayChoice *int) *huh.Group {
	fields := []huh.Field{
		huh.NewSelect[int]().Title("queries.json").Height(4).
			Options(
				huh.NewOption("Collect (30 days)", 30).Selected(*queriesDayChoice == 30),
				huh.NewOption("Collect (14 days)", 14).Selected(*queriesDayChoice == 14),
				huh.NewOption("Collect (7 days)", 7).Selected(*queriesDayChoice == 7),
				huh.NewOption("Collect (3 days)", 3).Selected(*queriesDayChoice == 3),
				huh.NewOption("Collect (1 day)", 1).Selected(*queriesDayChoice == 1),
				huh.NewOption("Skip", 0).Selected(*queriesDayChoice == 0),
			).Value(queriesDayChoice),
		huh.NewSelect[int]().Title("Queries Performance Data").Height(4).
			Options(
				huh.NewOption("Collect (30 days)", 30).Selected(*queriesPerfDayChoice == 30),
				huh.NewOption("Collect (14 days)", 14).Selected(*queriesPerfDayChoice == 14),
				huh.NewOption("Collect (7 days)", 7).Selected(*queriesPerfDayChoice == 7),
				huh.NewOption("Collect (3 days)", 3).Selected(*queriesPerfDayChoice == 3),
				huh.NewOption("Collect (1 day)", 1).Selected(*queriesPerfDayChoice == 1),
				huh.NewOption("Skip", 0).Selected(*queriesPerfDayChoice == 0),
			).Value(queriesPerfDayChoice),
		huh.NewSelect[int]().Title("Server logs").Height(4).
			Options(
				huh.NewOption("Collect (30 days)", 30).Selected(*serverDayChoice == 30),
				huh.NewOption("Collect (14 days)", 14).Selected(*serverDayChoice == 14),
				huh.NewOption("Collect (7 days)", 7).Selected(*serverDayChoice == 7),
				huh.NewOption("Collect (3 days)", 3).Selected(*serverDayChoice == 3),
				huh.NewOption("Collect (1 day)", 1).Selected(*serverDayChoice == 1),
				huh.NewOption("Skip", 0).Selected(*serverDayChoice == 0),
			).Value(serverDayChoice),
		huh.NewSelect[int]().Title("Tracker log").Height(4).
			Options(
				huh.NewOption("Collect (30 days)", 30).Selected(*trackerDayChoice == 30),
				huh.NewOption("Collect (14 days)", 14).Selected(*trackerDayChoice == 14),
				huh.NewOption("Collect (7 days)", 7).Selected(*trackerDayChoice == 7),
				huh.NewOption("Collect (3 days)", 3).Selected(*trackerDayChoice == 3),
				huh.NewOption("Collect (1 day)", 1).Selected(*trackerDayChoice == 1),
				huh.NewOption("Skip", 0).Selected(*trackerDayChoice == 0),
			).Value(trackerDayChoice),
		huh.NewSelect[int]().Title("Vacuum log").Height(4).
			Options(
				huh.NewOption("Collect (30 days)", 30).Selected(*vacuumDayChoice == 30),
				huh.NewOption("Collect (14 days)", 14).Selected(*vacuumDayChoice == 14),
				huh.NewOption("Collect (7 days)", 7).Selected(*vacuumDayChoice == 7),
				huh.NewOption("Collect (3 days)", 3).Selected(*vacuumDayChoice == 3),
				huh.NewOption("Collect (1 day)", 1).Selected(*vacuumDayChoice == 1),
				huh.NewOption("Skip", 0).Selected(*vacuumDayChoice == 0),
			).Value(vacuumDayChoice),
	}
	fields = append(fields, huh.NewConfirm().Title("Collect metadata refresh log").Value(&cfg.CollectMetaRefresh).Inline(true).Affirmative("Yes").Negative("No"))
	if cfg.Transport == "k8s" {
		fields = append(fields, huh.NewConfirm().Title("Collect K8s container logs").Value(&cfg.CollectContainerLogs).Inline(true).Affirmative("Yes").Negative("No"))
	}
	return huh.NewGroup(fields...).Title("Log collection").Description(" ")
}

// buildDiagnosticsToolsGroup builds the "Diagnostics tools" group with JVM tools + heap dump only.
// Log types and container logs have been moved to the "Logs Collection" group.
func buildDiagnosticsToolsGroup(selectedTools *[]string, diagDur *intStr, cfg *DiagnosisConfig) *huh.Group {
	fields := []huh.Field{
		huh.NewMultiSelect[string]().
			Title("Diagnostic tools to run (parallel on all nodes)").Height(5).
			Options(
				huh.NewOption("top (process snapshots)", "top"),
				huh.NewOption("async-profiler (CPU + native memory)", "async-profiler"),
				huh.NewOption("JFR (Java Flight Recorder)", "JFR"),
				huh.NewOption("jstack (thread dumps)", "jstack"),
			).Value(selectedTools),
		huh.NewInput().Title("Diagnostic tools time (sec)").Value(&diagDur.str).CharLimit(5).Validate(validateInt),
		huh.NewConfirm().Title("Heap dump (after diagnostic tools)").Value(&cfg.CollectHeapDump).Inline(true).Affirmative("Yes").Negative("No"),
	}
	return huh.NewGroup(fields...).Title("Diagnostics tools").Description(" ")
}

// syncDiagLogTypes maps the multi-select log type choices back to DiagnosisConfig booleans.
// Called both during form rendering (DescriptionFunc) and after form completion.
func syncDiagLogTypes(cfg *DiagnosisConfig, logTypes []string) {
	logSet := make(map[string]bool, len(logTypes))
	for _, lt := range logTypes {
		logSet[lt] = true
	}
	cfg.CollectQueriesJSON = logSet["queries"]
	cfg.CollectQueriesPerf = logSet["queries-perf"]
	cfg.CollectServerLogs = logSet["server"]
	cfg.CollectGCLogs = logSet["gc"]
	cfg.CollectHSErr = logSet["hserr"]
	cfg.CollectContainerLogs = logSet["container-logs"]
	cfg.CollectTrackerJSON = logSet["tracker"]
	cfg.CollectVacuumLog = logSet["vacuum"]
	cfg.CollectHiveDeprecated = logSet["hive"]
	cfg.CollectAcceleration = logSet["acceleration"]
	cfg.CollectAccess = logSet["access"]
	cfg.CollectMetaRefresh = logSet["meta-refresh"]
	cfg.CollectProblematicProfiles = logSet["profiles"]
	cfg.CollectKVStore = logSet["kvstore"]
}

// appendTransportAndPathFlags appends the transport-specific flags (namespace/coordinator/ssh-user/dremio-home)
// and path flags (log dirs, conf dir, rocksdb dir) shared by both CLI command builders.
func appendTransportAndPathFlags(parts []string, transport, namespace, k8sContext, kubeconfig, coordinator, executors, sshUser, dremioHome, coordinatorLogDir, executorLogDir, confDir, rocksdbDir, cont string) []string {
	switch transport {
	case "k8s", "local-k8s":
		// --kubeconfig appears on its own line above --namespace, only if user supplied one.
		if kubeconfig != "" {
			parts = append(parts, fmt.Sprintf("  --kubeconfig=%s"+cont, kubeconfig))
		}
		if transport == "k8s" {
			line := fmt.Sprintf("  --namespace=%s", namespace)
			if k8sContext != "" {
				line += fmt.Sprintf(" --context=%s", k8sContext)
			}
			parts = append(parts, line+cont)
		}
	case "ssh":
		parts = append(parts, fmt.Sprintf("  --coordinator=%s"+cont, coordinator))
		if executors != "" {
			parts = append(parts, fmt.Sprintf("  --executors=%s"+cont, executors))
		}
		if sshUser != "" {
			parts = append(parts, fmt.Sprintf("  --ssh-user=%s"+cont, sshUser))
		}
	case "local":
		parts = append(parts, fmt.Sprintf("  --dremio-home=%s"+cont, dremioHome))
	}
	if transport == "local" {
		parts = append(parts, fmt.Sprintf("  --local-log-dir=%s"+cont, coordinatorLogDir))
	} else {
		parts = append(parts, fmt.Sprintf("  --coordinator-log-dir=%s --executor-log-dir=%s"+cont, coordinatorLogDir, executorLogDir))
	}
	parts = append(parts, fmt.Sprintf("  --dremio-conf-dir=%s --dremio-rocksdb-dir=%s"+cont, confDir, rocksdbDir))
	return parts
}

// buildStandardCLICommand generates the equivalent CLI command for the current standard config.
func buildStandardCLICommand(cfg *StandardConfig, queryDays, queriesPerfDays, serverDays, trackerDays, vacuumDays int) string {
	bin, cont, _ := cliShellFormat()
	var parts []string

	parts = append(parts, bin+" collect "+cfg.Transport+" standard"+cont)
	parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Kubeconfig, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)

	// Log collection
	if serverDays > 0 {
		parts = append(parts, fmt.Sprintf("  --collect-server-logs=true --server-logs-num-days=%d"+cont, serverDays))
	} else {
		parts = append(parts, "  --collect-server-logs=false"+cont)
	}
	if trackerDays > 0 {
		parts = append(parts, fmt.Sprintf("  --collect-tracker-json=true --tracker-json-num-days=%d"+cont, trackerDays))
	} else {
		parts = append(parts, "  --collect-tracker-json=false"+cont)
	}
	if vacuumDays > 0 {
		parts = append(parts, fmt.Sprintf("  --collect-vacuum-log=true --vacuum-log-num-days=%d"+cont, vacuumDays))
	} else {
		parts = append(parts, "  --collect-vacuum-log=false"+cont)
	}
	parts = append(parts, fmt.Sprintf("  --collect-meta-refresh-log=%t"+cont, cfg.CollectMetaRefresh))
	if queryDays > 0 {
		parts = append(parts, fmt.Sprintf("  --collect-queries-json=true --queries-json-num-days=%d"+cont, queryDays))
	} else {
		parts = append(parts, "  --collect-queries-json=false"+cont)
	}
	if queriesPerfDays > 0 {
		parts = append(parts, fmt.Sprintf("  --collect-queries-perf-json=true --queries-perf-num-days=%d"+cont, queriesPerfDays))
	} else {
		parts = append(parts, "  --collect-queries-perf-json=false"+cont)
	}

	// Container logs — K8s only
	if cfg.Transport == "k8s" {
		parts = append(parts, fmt.Sprintf("  --collect-container-logs=%t"+cont, cfg.CollectContainerLogs))
	}

	// WLM and system tables — always emit --system-tables, even when empty,
	// so the user's deselection overrides the non-empty package default.
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t"+cont, cfg.CollectWLM))
	parts = append(parts, fmt.Sprintf("  --system-tables=%s", strings.Join(cfg.SystemTables, ",")))

	return strings.Join(parts, "\n")
}

// sliceContains reports whether s contains v.
func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// mapSelectedTools maps a slice of selected tool names to the corresponding
// Collect* booleans on DiagnosisConfig. Unselected tools are set to false.
func mapSelectedTools(selectedTools []string, cfg *DiagnosisConfig) {
	toolSet := make(map[string]bool, len(selectedTools))
	for _, t := range selectedTools {
		toolSet[t] = true
	}
	cfg.CollectJFR = toolSet["JFR"]
	cfg.CollectJStack = toolSet["jstack"]
	cfg.CollectTop = toolSet["top"]
	cfg.CollectAsyncProfiler = toolSet["async-profiler"]
	// CollectHeapDump is set directly by the Confirm field, not the tool multi-select.
}

func buildDiagnosisCLICommand(cfg *DiagnosisConfig, tools *[]string, nodes *[]string, days, diagDur, dateStart *string) string {
	bin, cont, patVar := cliShellFormat()
	var parts []string

	parts = append(parts, bin+" collect "+cfg.Transport+" diagnosis"+cont)
	parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Kubeconfig, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)
	// Start date + days on one line
	dateDaysLine := "  "
	if dateStart != nil && *dateStart != "" {
		dateDaysLine += fmt.Sprintf("--start-date=%s ", *dateStart)
	}
	if days != nil {
		dateDaysLine += fmt.Sprintf("--days=%s", *days)
	}
	parts = append(parts, strings.TrimRight(dateDaysLine, " ")+cont)

	// Diagnostic tools — derive from live selectedTools slice, not cfg booleans
	hasJFR := tools != nil && sliceContains(*tools, "JFR")
	hasJStack := tools != nil && sliceContains(*tools, "jstack")
	hasTop := tools != nil && sliceContains(*tools, "top")
	hasAP := tools != nil && sliceContains(*tools, "async-profiler")
	hasHeap := cfg.CollectHeapDump

	parts = append(parts, fmt.Sprintf("  --diag-jfr=%t --diag-jstack=%t --diag-top=%t"+cont, hasJFR, hasJStack, hasTop))
	diagLine := fmt.Sprintf("  --diag-async-profiler=%t", hasAP)
	if diagDur != nil {
		diagLine += fmt.Sprintf(" --diag-time-seconds=%s", *diagDur)
	}
	diagLine += fmt.Sprintf(" --diag-heap-dump=%t", hasHeap)
	parts = append(parts, diagLine+cont)

	// Log collection toggles
	parts = append(parts, fmt.Sprintf("  --collect-server-logs=%t --collect-queries-json=%t --collect-queries-perf-json=%t"+cont, cfg.CollectServerLogs, cfg.CollectQueriesJSON, cfg.CollectQueriesPerf))
	parts = append(parts, fmt.Sprintf("  --collect-tracker-json=%t --collect-vacuum-log=%t --collect-hs-err-files=%t"+cont, cfg.CollectTrackerJSON, cfg.CollectVacuumLog, cfg.CollectHSErr))
	parts = append(parts, fmt.Sprintf("  --collect-gc-logs=%t --collect-acceleration-log=%t --collect-access-log=%t"+cont, cfg.CollectGCLogs, cfg.CollectAcceleration, cfg.CollectAccess))
	// Hive-deprecated + meta-refresh (+ K8s container logs) on one line
	logToggleLine := fmt.Sprintf("  --collect-hive-deprecated-log=%t --collect-meta-refresh-log=%t", cfg.CollectHiveDeprecated, cfg.CollectMetaRefresh)
	if cfg.Transport == "k8s" {
		logToggleLine += fmt.Sprintf(" --collect-container-logs=%t", cfg.CollectContainerLogs)
	}
	parts = append(parts, logToggleLine+cont)

	// Node selection
	if nodes != nil && len(*nodes) > 0 {
		parts = append(parts, fmt.Sprintf("  --nodes=%s"+cont, strings.Join(*nodes, ",")))
	}

	// PAT-dependent collections — always emit --system-tables, even when empty,
	// so the user's deselection overrides the non-empty package default.
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t --collect-kvstore-report=%t --collect-problematic-profiles=%t"+cont, cfg.CollectWLM, cfg.CollectKVStore, cfg.CollectProblematicProfiles))
	parts = append(parts, fmt.Sprintf("  --system-tables=%s"+cont, strings.Join(cfg.SystemTables, ",")))

	// Endpoint and PAT
	if cfg.PATToken != "" {
		parts = append(parts, fmt.Sprintf("  --dremio-endpoint=%s --allow-insecure-ssl=%t"+cont, cfg.DremioEndpoint, cfg.AllowInsecureSSL))
		parts = append(parts, "  --dremio-pat-token="+patVar)
	} else {
		// Remove trailing continuation from last line
		last := len(parts) - 1
		parts[last] = strings.TrimSuffix(parts[last], cont)
	}

	return strings.Join(parts, "\n")
}

// escapeForHuh escapes a CLI command string so huh's glamour markdown renderer
// displays it correctly. Backslashes, backticks, and underscores are doubled/escaped.
func escapeForHuh(cmd string) string {
	// Double backslashes so glamour's markdown doesn't treat them as escape characters.
	cmd = strings.ReplaceAll(cmd, "\\", "\\\\")
	// Escape underscores so glamour doesn't treat them as italic markers.
	cmd = strings.ReplaceAll(cmd, "_", "\\_")
	// Escape backticks so glamour doesn't treat them as inline code markers.
	cmd = strings.ReplaceAll(cmd, "`", "\\`")
	return cmd
}

// cliShellFormat returns OS-appropriate command formatting.
func cliShellFormat() (binary, continuation, patEnvVar string) {
	if runtime.GOOS == "windows" {
		// PowerShell uses backtick for line continuation.
		return ".\\ddc.exe", " `", "$env:DDC_PAT_TOKEN"
	}
	return "ddc", " \\", "$DDC_PAT_TOKEN"
}

// ValidatePAT performs an HTTP request to the Dremio API to check if the PAT token is valid.
// Returns "" on success, or an error message on failure.
func ValidatePAT(endpoint, pat string, allowInsecure bool) string {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: allowInsecure, //nolint:gosec
			},
		},
	}

	url := strings.TrimRight(endpoint, "/") + "/api/v3/catalog"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Sprintf("PAT validation failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+pat)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("PAT validation failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "PAT validated successfully"
	}
	return fmt.Sprintf("PAT validation failed: HTTP %d", resp.StatusCode)
}
