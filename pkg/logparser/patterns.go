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

// Package logparser scans Dremio server.log files for job IDs associated with
// failures, OOM events, and cancellations. Supports Dremio 24.x–26.x log formats.
package logparser

import "regexp"

// Pattern holds a compiled regex that matches a log line of interest,
// along with metadata for categorisation.
type Pattern struct {
	Name     string
	Category string
	Regex    *regexp.Regexp
	Exclude  bool // when true, matched IDs are added to the exclusion set instead of matches
}

// uuidHex is the raw regex fragment for a UUID.
const uuidHex = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

// threadPattern matches bracketed thread names containing a UUID
const threadPattern = `\[(?i)(` + uuidHex + `)(?::[\w-]+)*\]`

// buildPatterns returns the set of line-matching patterns. Each pattern
// identifies a category of interesting log line and MUST include exactly one
// capturing group that matches the JobId/QueryId.
func buildPatterns() []Pattern {
	compile := func(name, category, expr string) Pattern {
		return Pattern{
			Name:     name,
			Category: category,
			Regex:    regexp.MustCompile(expr),
		}
	}
	exclude := func(name, category, expr string) Pattern {
		return Pattern{
			Name:     name,
			Category: category,
			Regex:    regexp.MustCompile(expr),
			Exclude:  true,
		}
	}

	return []Pattern{
		// OOM patterns (fallback matching threadname)
		compile("oom_error", "oom", threadPattern+`.*?(?i)OUT_OF_MEMORY`),
		compile("oom_exception", "oom", threadPattern+`.*?(?i)OutOfMemoryException`),
		compile("oom_exceeded_memory", "oom", threadPattern+`.*?(?i)exceeded\s+(?:the\s+)?memory\s+limits`),

		// Exact Query failure patterns from architecture-plan
		compile("query_failed", "query_failure", `(?i)Fragment\s+(`+uuidHex+`):\d+:\d+\s+failed`),
		compile("abandoned_job", "query_failure", `(?i)Failing\s+abandoned\s+job\s+(`+uuidHex+`)`),
		compile("failed_to_admit", "query_failure", `(?i)Query\s+(`+uuidHex+`)\s+was\s+marked\s+with\s+failedToAdmit\s+as\s+true`),

		// Cancellation patterns
		compile("query_cancelled", "cancellation", `(?i)Canceling\s+query\s+(`+uuidHex+`)`),
		compile("sending_cancellation", "cancellation", `(?i)Sending\s+cancellation\s+for\s+query\s+(`+uuidHex+`)`),

		// Heap monitor patterns
		compile("heap_monitor_cancel", "heap_monitor", `(?i)HEAP_MONITOR\s+initiated\s+canceling\s+of\s+query\s+(`+uuidHex+`)`),
		compile("csv_heap_dump", "heap_monitor", `(?m)^"?(`+uuidHex+`)"?,`),

		// Fallback handlers for existing generalized tests
		compile("fragment_tracker_failed", "query_failure", `(?i)FragmentTracker.*query\s+(`+uuidHex+`)\s+failed`),
		compile("system_error", "query_failure", `(?i)SYSTEM\s+ERROR.*Fragment\s+(`+uuidHex+`):\d+:\d+`),
		compile("query_cancelled_fallback", "cancellation", `(?i)Query\s+(`+uuidHex+`)\s+cancell?ed`),
		compile("query_canceled_fallback", "cancellation", `(?i)Query\s+(`+uuidHex+`).*canceled`),
		compile("query_failed_fallback", "query_failure", `(?i)Query\s+(`+uuidHex+`)\s+failed`),
		compile("heap_monitor_dump", "heap_monitor", `(?i)HeapMonitorThread.*dumping.*query\s+(`+uuidHex+`)`),
		compile("memory_allocation_util", "planning_failure", threadPattern+`.*?(?i)MemoryAllocationUtilities`),
		compile("message_body_oom", "query_failure", `(?i)Query\s+(`+uuidHex+`)\s+failed\s+with\s+OOM`),

		// Exclusion patterns — queries with global CANCELLED status are excluded.
		// Cancelled fragments of FAILED queries are kept because those won't have outcome: CANCELLED.
		exclude("outcome_cancelled", "exclusion", `(?i)Query:\s+(`+uuidHex+`);\s+outcome:\s+CANCELLED`),
	}
}

// extractJobIDsFromMatches pulls the UUID explicitly from the regex submatch
func extractJobIDsFromMatches(matches []string) []string {
	seen := map[string]bool{}
	var ids []string
	if len(matches) <= 1 {
		return ids
	}
	for i := 1; i < len(matches); i++ {
		m := matches[i]
		if len(m) == 36 && m[8] == '-' && m[13] == '-' && m[18] == '-' && m[23] == '-' {
			lower := toLowerASCII(m)
			if !seen[lower] {
				seen[lower] = true
				ids = append(ids, lower)
			}
		}
	}
	return ids
}

// toLowerASCII lowercases ASCII hex characters without allocating via strings.ToLower.
func toLowerASCII(s string) string {
	buf := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'F' {
			c += 'a' - 'A'
		}
		buf[i] = c
	}
	return string(buf)
}
