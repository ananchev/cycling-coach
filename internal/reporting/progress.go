package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cycling-coach/internal/storage"
)

type ProgressKPIDefinition struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Explanation string `json:"explanation"`
}

type progressPromptPayload struct {
	Period                       string               `json:"period"`
	PriorPeriod                  string               `json:"prior_period"`
	AerobicEfficiency            progressPromptMetric `json:"aerobic_efficiency"`
	EnduranceDurability          progressPromptMetric `json:"endurance_durability"`
	ActiveCalories               progressPromptMetric `json:"active_calories"`
	CumulativeTSS                progressPromptMetric `json:"cumulative_tss"`
	CumulativeTRIMP              progressPromptMetric `json:"cumulative_trimp"`
	AverageIntensityFactor       progressPromptMetric `json:"average_intensity_factor"`
	CompletionRate               progressPromptMetric `json:"completion_rate"`
	AverageWeightKG              progressPromptMetric `json:"average_weight_kg"`
}

type progressPromptMetric struct {
	Current  *float64               `json:"current,omitempty"`
	Prior    *float64               `json:"prior,omitempty"`
	Delta    *float64               `json:"delta,omitempty"`
	DeltaPct *float64               `json:"delta_pct,omitempty"`
	Trend    storage.ProgressTrend  `json:"trend"`
}

func ProgressKPIDefinitions() []ProgressKPIDefinition {
	return []ProgressKPIDefinition{
		{
			Key:         "aerobic_efficiency",
			Title:       "Aerobic Efficiency (EF)",
			Explanation: "Power produced per unit of heart-rate cost. Higher EF usually means better aerobic economy.",
		},
		{
			Key:         "endurance_durability",
			Title:       "Endurance Durability (Decoupling)",
			Explanation: "How much cardiovascular drift appears over long rides. Lower decoupling usually means better durability.",
		},
		{
			Key:         "active_calories",
			Title:       "Active Calorie Burn",
			Explanation: "Sum of Wahoo-reported workout calories across the selected period; useful as a rough volume signal, not an exact metabolic measure.",
		},
		{
			Key:         "cumulative_tss",
			Title:       "Training Load (TSS)",
			Explanation: "External workload accumulated across the selected period.",
		},
		{
			Key:         "cumulative_trimp",
			Title:       "Training Load (TRIMP)",
			Explanation: "Internal cardiovascular load accumulated across the selected period.",
		},
		{
			Key:         "average_intensity_factor",
			Title:       "Training Intensity (Average IF)",
			Explanation: "Average workout intensity relative to your FTP. Lower values usually mean more endurance riding; higher values mean the period included more hard work.",
		},
		{
			Key:         "completion_rate",
			Title:       "Consistency (Completion Rate)",
			Explanation: "Share of days in the period that contained a real workout rather than only an empty placeholder day.",
		},
		{
			Key:         "average_weight_kg",
			Title:       "Average Weight",
			Explanation: "Average recorded body weight during the selected period.",
		},
	}
}

// GenerateProgressAnalysis builds an aggregated KPI snapshot for the selected
// period, sends it to Claude together with the athlete profile, and persists
// the single saved interpretation row.
func (o *Orchestrator) GenerateProgressAnalysis(ctx context.Context, from, to time.Time, aerobicOnlyEF bool) (*storage.ProgressAnalysis, error) {
	cp, ok := o.provider.(*ClaudeProvider)
	if !ok {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: provider does not support raw calls (not a *ClaudeProvider)")
	}

	snapshot, err := storage.BuildProgressSnapshot(o.db, from, to, aerobicOnlyEF)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: snapshot: %w", err)
	}

	profile, err := os.ReadFile(o.profilePath)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: read profile: %w", err)
	}

	payload := buildProgressPromptPayload(snapshot)
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: marshal payload: %w", err)
	}
	systemPrompt := progressAnalysisSystemPrompt
	userPrompt := buildProgressAnalysisUserPrompt(string(profile), string(payloadJSON))

	narrative, err := cp.CallRaw(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: call Claude: %w", err)
	}

	analysis := &storage.ProgressAnalysis{
		PeriodFrom:   snapshot.SelectedRange.From,
		PeriodTo:     snapshot.SelectedRange.To,
		SnapshotJSON: string(payloadJSON),
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Narrative:    strings.TrimSpace(narrative),
	}
	if err := storage.UpsertProgressAnalysis(o.db, analysis); err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: save: %w", err)
	}

	saved, err := storage.GetProgressAnalysis(o.db)
	if err != nil {
		return nil, fmt.Errorf("reporting.Orchestrator.GenerateProgressAnalysis: reload: %w", err)
	}
	return saved, nil
}

func buildProgressPromptPayload(snapshot *storage.ProgressSnapshot) progressPromptPayload {
	return progressPromptPayload{
		Period:                 formatDateRange(snapshot.SelectedRange.From, snapshot.SelectedRange.To),
		PriorPeriod:            formatDateRange(snapshot.PriorRange.From, snapshot.PriorRange.To),
		AerobicEfficiency:      toPromptMetric(snapshot.AerobicEfficiency),
		EnduranceDurability:    toPromptMetric(snapshot.EnduranceDurability),
		ActiveCalories:         toPromptMetric(snapshot.ActiveCalories),
		CumulativeTSS:          toPromptMetric(snapshot.CumulativeTSS),
		CumulativeTRIMP:        toPromptMetric(snapshot.CumulativeTRIMP),
		AverageIntensityFactor: toPromptMetric(snapshot.AverageIF),
		CompletionRate:         toPromptMetric(snapshot.CompletionRate),
		AverageWeightKG:        toPromptMetric(snapshot.AverageWeightKG),
	}
}

func buildProgressAnalysisUserPrompt(athleteProfile, payloadJSON string) string {
	return fmt.Sprintf(`Please analyze the following long-term KPI trends.

Use the athlete profile as the primary coaching context. The profile describes the athlete's current phase, goals, constraints, warning flags, and coaching expectations.

KPI quick reference:
- Aerobic Efficiency (EF): power produced per unit of heart-rate cost. Higher is usually better.
- Endurance Durability (Decoupling): cardiovascular drift over long rides. Lower is usually better.
- Active Calorie Burn: total recorded workout calories across the selected period.
- Cumulative TSS: external training load across the selected period.
- Cumulative TRIMP: internal cardiovascular load across the selected period.
- Average IF: average workout intensity relative to threshold.
- Completion Rate: percentage of days that contained a real workout rather than only a placeholder day.
- Average Weight: average recorded body weight in the selected period.

Here is the athlete's current profile and coaching context:
<athlete_profile>
%s
</athlete_profile>

Here is the aggregated KPI data for the selected period compared to the prior period:
<kpi_data>
%s
</kpi_data>

Provide a concise, 3-part analysis formatted in Markdown:
1. **Executive Summary:** A 2-sentence overview of how the athlete is responding to the training load.
2. **Key Physiological Shifts:** Explain what the KPI changes suggest about aerobic engine, durability, consistency, and load balance.
3. **Coaching Recommendation:** Suggest 1-2 macro adjustments for the upcoming block based on the athlete's phase and the observed trends.

Do not invent metrics that are not present. Keep the analysis practical and specific.`, athleteProfile, payloadJSON)
}

func toPromptMetric(m storage.ProgressMetric) progressPromptMetric {
	return progressPromptMetric{
		Current:  m.Current,
		Prior:    m.Prior,
		Delta:    m.Delta,
		DeltaPct: m.DeltaPct,
		Trend:    m.Trend,
	}
}

func formatDateRange(from, to time.Time) string {
	return fmt.Sprintf("%s to %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
}

const progressAnalysisSystemPrompt = `You are an expert cycling coach analyzing a physiological progress snapshot for your athlete.

You communicate clearly, directly, and objectively. Ground your interpretation in sports science concepts such as TSS, TRIMP, intensity factor, aerobic efficiency, durability, and consistency.

Trend direction is raw only:
- "up" means the current period value is higher than the prior period value
- "down" means the current period value is lower than the prior period value
- "steady" means the change is small enough to be treated as stable

Do not assume that every up or down movement is automatically good or bad. Judge it in the context of the athlete profile, training phase, and the metric itself.`
