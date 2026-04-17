package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// ProfilePatchOutput is the JSON structure Claude returns for a weekly profile patch.
type ProfilePatchOutput struct {
	RecentWeeksRow string            `json:"recent_weeks_row"`
	Milestones     map[string]string `json:"milestones"`
	LastUpdated    string            `json:"last_updated"`
}

// PatchProfileResult is returned by PatchProfile on success.
type PatchProfileResult struct {
	BackupPath string
}

// PatchProfile performs a lightweight, surgical update of the athlete profile
// after a weekly report is generated. It updates only:
//   - "## Recent training weeks" — appends a new row, drops oldest if >8
//   - "## Stelvio readiness milestones" — ticks milestones whose criteria were met
//   - "## Last Updated" — sets today's date
//
// The full profile is NOT rewritten; only these three sections are patched.
func PatchProfile(ctx context.Context, profilePath string, provider *ClaudeProvider, reportNarrative string, weekMetrics WeekMetrics) (*PatchProfileResult, error) {
	currentProfile, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: read profile: %w", err)
	}

	prompt := buildPatchPrompt(string(currentProfile), reportNarrative, weekMetrics)

	rawJSON, err := provider.CallRaw(ctx, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: call Claude: %w", err)
	}

	var patch ProfilePatchOutput
	if err := json.Unmarshal([]byte(rawJSON), &patch); err != nil {
		// Try stripping markdown code fences
		cleaned := stripCodeFence(rawJSON)
		if err2 := json.Unmarshal([]byte(cleaned), &patch); err2 != nil {
			return nil, fmt.Errorf("reporting.PatchProfile: parse patch JSON: %w (raw: %s)", err, rawJSON)
		}
	}

	patched, err := ApplyProfilePatch(string(currentProfile), patch)
	if err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: apply patch: %w", err)
	}

	if err := validateEvolvedProfile(patched); err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: validation after patch: %w", err)
	}

	backupPath := profileBackupPath(profilePath)
	if err := os.WriteFile(backupPath, currentProfile, 0644); err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: backup: %w", err)
	}

	if err := os.WriteFile(profilePath, []byte(patched), 0644); err != nil {
		return nil, fmt.Errorf("reporting.PatchProfile: write patched profile: %w", err)
	}

	slog.Info("reporting: profile patched after block close", "backup", backupPath)
	return &PatchProfileResult{BackupPath: backupPath}, nil
}

// WeekMetrics holds the raw week-level numbers passed to the patch prompt.
type WeekMetrics struct {
	WeekEnding string
	TotalTSS   float64
	KeySession string
	AvgDrift   float64
}

func buildPatchPrompt(currentProfile, reportNarrative string, m WeekMetrics) string {
	return fmt.Sprintf(`You are updating a cycling athlete profile after a weekly training block was closed.

Your task is to produce a small JSON patch for exactly three sections of the profile. Do NOT output the full profile — only the JSON object described below.

## Current athlete profile

%s

## Just-generated weekly report narrative

%s

## Raw week metrics

- Week ending: %s
- Total TSS: %.0f
- Key session: %s
- Average HR drift: %.1f%%

## Instructions

Produce a JSON object with exactly three fields:

1. "recent_weeks_row": A single markdown table row to append to the "## Recent training weeks" table.
   Format: "| <week ending date> | <TSS> | <key session summary> | <avg drift>%% | <brief note> |"
   Keep the key session summary under 30 characters. The note should be a short forward-looking comment.

2. "milestones": An object mapping milestone numbers (as strings "1" through "7") to their updated status.
   Use "✅ Met" when the report data shows the milestone criteria were clearly achieved this week.
   Use "⬜ Not yet" for milestones not yet met.
   Use "🟡 Close" when the data shows the athlete is within reach but hasn't definitively hit the criterion.
   Be conservative — only mark "✅ Met" when the evidence is unambiguous.

3. "last_updated": Today's date in YYYY-MM-DD format.

Respond with ONLY the JSON object. No explanation, no code fences, no preamble.`, currentProfile, reportNarrative, m.WeekEnding, m.TotalTSS, m.KeySession, m.AvgDrift)
}

// ApplyProfilePatch surgically replaces the three target sections in the profile.
func ApplyProfilePatch(profile string, patch ProfilePatchOutput) (string, error) {
	var err error

	profile, err = patchRecentWeeks(profile, patch.RecentWeeksRow)
	if err != nil {
		return "", fmt.Errorf("recent weeks: %w", err)
	}

	profile, err = patchMilestones(profile, patch.Milestones)
	if err != nil {
		return "", fmt.Errorf("milestones: %w", err)
	}

	profile, err = patchLastUpdated(profile, patch.LastUpdated)
	if err != nil {
		return "", fmt.Errorf("last updated: %w", err)
	}

	return profile, nil
}

// patchRecentWeeks appends a new row to the Recent training weeks table.
// If there are already 8 data rows, the oldest (first data row) is dropped.
func patchRecentWeeks(profile, newRow string) (string, error) {
	const heading = "## Recent training weeks"
	sectionStart, sectionEnd, found := FindSection(profile, heading)
	if !found {
		return "", fmt.Errorf("section %q not found in profile", heading)
	}

	section := profile[sectionStart:sectionEnd]
	lines := strings.Split(section, "\n")

	// Find the table structure: header row, separator row, then data rows.
	var sepIdx int
	var dataStartIdx int
	foundTable := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "| Week ending") || strings.HasPrefix(trimmed, "|----------") {
			if strings.HasPrefix(trimmed, "|---") {
				sepIdx = i
				dataStartIdx = i + 1
				foundTable = true
			}
		}
	}

	if !foundTable {
		return "", fmt.Errorf("could not find table structure in Recent training weeks section")
	}

	// Collect existing data rows (skip placeholder rows that start with "| *")
	var dataRows []string
	for i := dataStartIdx; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || trimmed == "---" || strings.HasPrefix(trimmed, "##") {
			break
		}
		// Skip placeholder rows
		if strings.Contains(trimmed, "*(no data yet") {
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			dataRows = append(dataRows, lines[i])
		}
	}

	// Append new row
	dataRows = append(dataRows, newRow)

	// Drop oldest if >8
	if len(dataRows) > 8 {
		dataRows = dataRows[len(dataRows)-8:]
	}

	// Rebuild section
	var rebuilt strings.Builder
	for i := 0; i <= sepIdx; i++ {
		rebuilt.WriteString(lines[i])
		rebuilt.WriteString("\n")
	}
	for _, row := range dataRows {
		rebuilt.WriteString(row)
		rebuilt.WriteString("\n")
	}

	return profile[:sectionStart] + rebuilt.String() + profile[sectionEnd:], nil
}

// patchMilestones replaces the milestone status values in the Stelvio readiness milestones table.
func patchMilestones(profile string, milestones map[string]string) (string, error) {
	const heading = "## Stelvio readiness milestones"
	sectionStart, sectionEnd, found := FindSection(profile, heading)
	if !found {
		return "", fmt.Errorf("section %q not found in profile", heading)
	}

	section := profile[sectionStart:sectionEnd]
	lines := strings.Split(section, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "| #") || strings.HasPrefix(trimmed, "|---") {
			continue
		}
		// Parse milestone number from the row: "| 1 | description | status |"
		parts := strings.SplitN(trimmed, "|", -1)
		if len(parts) < 4 {
			continue
		}
		num := strings.TrimSpace(parts[1])
		if newStatus, ok := milestones[num]; ok {
			// Replace the last cell (status) with the new value
			// Rebuild the row preserving the milestone number and description
			oldParts := splitTableRow(trimmed)
			if len(oldParts) >= 3 {
				oldParts[len(oldParts)-1] = " " + newStatus + " "
				lines[i] = "|" + strings.Join(oldParts, "|") + "|"
			}
		}
	}

	rebuilt := strings.Join(lines, "\n")
	return profile[:sectionStart] + rebuilt + profile[sectionEnd:], nil
}

// patchLastUpdated replaces the content of the "## Last Updated" section.
func patchLastUpdated(profile, newDate string) (string, error) {
	const heading = "## Last Updated"
	sectionStart, sectionEnd, found := FindSection(profile, heading)
	if !found {
		return "", fmt.Errorf("section %q not found in profile", heading)
	}

	section := profile[sectionStart:sectionEnd]
	lines := strings.Split(section, "\n")

	// Find the bold date line and replace it
	foundDate := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "**") && strings.Contains(line, "-") {
			lines[i] = fmt.Sprintf("**%s**", newDate)
			foundDate = true
			break
		}
	}
	if !foundDate {
		// Append date after heading + comment lines
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if i > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "<!--") && !strings.HasPrefix(trimmed, "##") {
				lines[i] = fmt.Sprintf("**%s**", newDate)
				break
			}
		}
	}

	rebuilt := strings.Join(lines, "\n")
	return profile[:sectionStart] + rebuilt + profile[sectionEnd:], nil
}

// FindSection returns the start and end byte offsets of a markdown section.
// The section starts at the heading line and ends just before the next same-level
// or higher heading, or at end of string. It includes trailing whitespace.
func FindSection(profile, heading string) (int, int, bool) {
	idx := strings.Index(profile, heading)
	if idx == -1 {
		return 0, 0, false
	}

	// Find the start of this heading line
	lineStart := idx
	if lineStart > 0 {
		prevNewline := strings.LastIndex(profile[:lineStart], "\n")
		if prevNewline >= 0 {
			lineStart = prevNewline + 1
		} else {
			lineStart = 0
		}
	}

	// Find the next section heading (## ) after this one
	rest := profile[idx+len(heading):]
	headingLevel := countHeadingLevel(profile[lineStart:])

	nextIdx := -1
	searchFrom := 0
	for {
		nlIdx := strings.Index(rest[searchFrom:], "\n")
		if nlIdx == -1 {
			break
		}
		nlIdx += searchFrom
		lineAfterNl := rest[nlIdx+1:]
		if len(lineAfterNl) > 0 && lineAfterNl[0] == '#' {
			nextLevel := countHeadingLevel(lineAfterNl)
			if nextLevel > 0 && nextLevel <= headingLevel {
				nextIdx = nlIdx + 1
				break
			}
		}
		// Also check for horizontal rules (---) as section separators
		trimmedLine := strings.TrimSpace(lineAfterNl)
		if len(trimmedLine) >= 3 && trimmedLine == "---" {
			// Check if a heading follows the ---
			afterRule := lineAfterNl[strings.Index(lineAfterNl, "\n")+1:]
			if len(afterRule) > 0 && afterRule[0] == '#' {
				nextLevel := countHeadingLevel(afterRule)
				if nextLevel > 0 && nextLevel <= headingLevel {
					nextIdx = nlIdx + 1
					break
				}
			}
		}
		searchFrom = nlIdx + 1
	}

	sectionEnd := len(profile)
	if nextIdx >= 0 {
		sectionEnd = idx + len(heading) + nextIdx
	}

	return lineStart, sectionEnd, true
}

func countHeadingLevel(line string) int {
	level := 0
	for _, ch := range line {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level > 0 && level < len(line) && line[level] == ' ' {
		return level
	}
	return 0
}

// splitTableRow splits a markdown table row like "| a | b | c |" into ["a", "b", "c"].
// It returns the inner cells without the outer pipes.
func splitTableRow(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	return parts
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// ComputeWeekMetrics derives the WeekMetrics summary from a ReportInput.
func ComputeWeekMetrics(input *ReportInput) WeekMetrics {
	wm := WeekMetrics{
		WeekEnding: input.WeekEnd.Format("Jan 2, 2006"),
	}

	var totalTSS float64
	var totalDrift float64
	var driftCount int
	var bestTSS float64
	var bestSession string

	for _, r := range input.Rides {
		if r.TSS != nil {
			totalTSS += *r.TSS
			if *r.TSS > bestTSS {
				bestTSS = *r.TSS
				dur := ""
				if r.DurationMin != nil {
					dur = fmt.Sprintf("%.0fmin", *r.DurationMin)
				}
				pwr := ""
				if r.AvgPower != nil {
					pwr = fmt.Sprintf("%.0fW", *r.AvgPower)
				}
				bestSession = fmt.Sprintf("%s %s %s", r.Date.Format(time.DateOnly), dur, pwr)
			}
		}
		if r.HRDriftPct != nil {
			totalDrift += *r.HRDriftPct
			driftCount++
		}
	}

	wm.TotalTSS = totalTSS
	wm.KeySession = bestSession
	if driftCount > 0 {
		wm.AvgDrift = totalDrift / float64(driftCount)
	}

	return wm
}
