package internal

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds and returns the MCP server with all tools and prompts registered.
// Capability registration happens here (§6 tools, §7 prompts).
// Tool implementations are stubs — replaced by real mTLS calls in WP6.
// Prompt implementations are stubs — replaced by real grounding logic in WP7.
func NewServer(cfg *Config) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "cycling-coach-mcp",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: "Read-only MCP server for a personal cycling training assistant. " +
			"Provides access to the athlete's workouts, metrics, zone data, notes, " +
			"body metrics, training reports, progress KPIs, and athlete profile.",
	})

	registerTools(s, cfg)
	registerPrompts(s)
	return s
}

// ── Tool input types (§6) ─────────────────────────────────────────────────────
// All optional fields use pointer types so the SDK schema marks them as not required.

type noInput struct{}

type windowInput struct {
	From     *string `json:"from,omitempty"     jsonschema:"Start date (YYYY-MM-DD). Defaults to today minus MCP_DEFAULT_WINDOW_DAYS."`
	To       *string `json:"to,omitempty"       jsonschema:"End date (YYYY-MM-DD). Defaults to today."`
	LastDays *int    `json:"last_days,omitempty" jsonschema:"Convenience shorthand: from = today minus last_days, to = today."`
}

type blockContextInput struct {
	From     *string `json:"from,omitempty"     jsonschema:"Start date (YYYY-MM-DD)."`
	To       *string `json:"to,omitempty"       jsonschema:"End date (YYYY-MM-DD)."`
	LastDays *int    `json:"last_days,omitempty" jsonschema:"Convenience shorthand for date range."`
	Current  *bool   `json:"current,omitempty"  jsonschema:"When true, infer the window from the day after the latest saved report to today."`
}

type progressInput struct {
	From          *string `json:"from,omitempty"            jsonschema:"Start date for the selected period (YYYY-MM-DD)."`
	AerobicOnlyEF *bool   `json:"aerobic_only_ef,omitempty" jsonschema:"When true, compute efficiency factor only for aerobic rides (IF < 0.8). Default true."`
}

type listWorkoutsInput struct {
	From     *string `json:"from,omitempty"     jsonschema:"Start date (YYYY-MM-DD)."`
	To       *string `json:"to,omitempty"       jsonschema:"End date (YYYY-MM-DD)."`
	LastDays *int    `json:"last_days,omitempty" jsonschema:"Convenience shorthand for date range."`
	Type     *string `json:"type,omitempty"     jsonschema:"Filter by workout type (e.g. Cycling, Running)."`
	Limit    *int    `json:"limit,omitempty"    jsonschema:"Maximum number of rows to return (max 200)."`
}

type getWorkoutInput struct {
	ID      *int64  `json:"id,omitempty"       jsonschema:"Internal workout row ID."`
	WahooID *string `json:"wahoo_id,omitempty" jsonschema:"Wahoo external workout ID (exact match)."`
}

type listNotesInput struct {
	From      *string `json:"from,omitempty"       jsonschema:"Start date (YYYY-MM-DD)."`
	To        *string `json:"to,omitempty"         jsonschema:"End date (YYYY-MM-DD)."`
	LastDays  *int    `json:"last_days,omitempty"  jsonschema:"Convenience shorthand for date range."`
	Type      *string `json:"type,omitempty"       jsonschema:"Filter by note type: ride, note, or weight."`
	WorkoutID *int64  `json:"workout_id,omitempty" jsonschema:"Filter notes linked to a specific workout ID."`
	Query     *string `json:"query,omitempty"      jsonschema:"Case-insensitive substring filter on note text."`
	Limit     *int    `json:"limit,omitempty"      jsonschema:"Maximum number of rows to return (max 200)."`
}

type listReportsInput struct {
	Type  *string `json:"type,omitempty"  jsonschema:"Filter by report type: weekly_report, weekly_plan, or all (default all)."`
	Limit *int    `json:"limit,omitempty" jsonschema:"Maximum number of rows to return. Default 10."`
}

type getReportInput struct {
	ID         *int64  `json:"id,omitempty"          jsonschema:"Internal report row ID."`
	Type       *string `json:"type,omitempty"        jsonschema:"Report type (weekly_report or weekly_plan). Used with week_start."`
	WeekStart  *string `json:"week_start,omitempty"  jsonschema:"Week start date (YYYY-MM-DD). Used with type."`
	LatestType *string `json:"latest_type,omitempty" jsonschema:"Return the most recent report of this type (weekly_report or weekly_plan)."`
}

// ── Tool registration (§6) ────────────────────────────────────────────────────

func registerTools(s *mcp.Server, _ *Config) {
	addStubTool[noInput](s, &mcp.Tool{
		Name:        "get_athlete_profile",
		Description: "Returns the athlete's profile as markdown. Primary grounding for climb, equipment, and fitness-level advice.",
	})

	addStubTool[noInput](s, &mcp.Tool{
		Name:        "get_zone_config",
		Description: "Returns FTP, HR max, and all HR/power zone upper bounds. Use before any power- or HR-zone interpretation.",
	})

	addStubTool[blockContextInput](s, &mcp.Tool{
		Name:        "get_training_block_context",
		Description: "Returns a full assembled coaching view of the requested training period — the same grounding the report generator uses. Default tool for block-quality discussion and planning. Pass current=true to get the in-progress block since the last report.",
	})

	addStubTool[progressInput](s, &mcp.Tool{
		Name:        "get_progress",
		Description: "Returns KPIs for the selected period vs the equal-length prior period (aerobic efficiency, TSS, TRIMP, IF, completion rate, weight), plus weekly load series and any saved AI narrative.",
	})

	addStubTool[listWorkoutsInput](s, &mcp.Tool{
		Name:        "list_workouts",
		Description: "Returns a compact list of workouts in the requested date range with key summary metrics. Truncates at MCP_MAX_ROWS with a hint to narrow the range.",
	})

	addStubTool[getWorkoutInput](s, &mcp.Tool{
		Name:        "get_workout",
		Description: "Returns full detail for a single workout: aggregates, power/HR zone percentages, cadence bands, zone timeline markdown, and any linked ride or general notes.",
	})

	addStubTool[listNotesInput](s, &mcp.Tool{
		Name:        "list_notes",
		Description: "Returns athlete notes (ride RPE, general notes, body metrics) filtered by date range, type, workout linkage, or text substring.",
	})

	addStubTool[windowInput](s, &mcp.Tool{
		Name:        "get_body_metrics",
		Description: "Returns body metric readings (weight, body fat, muscle mass, body water, BMR) for the requested period, with first-to-last deltas. Wyze-deduplication aware.",
	})

	addStubTool[listReportsInput](s, &mcp.Tool{
		Name:        "list_reports",
		Description: "Returns a list of training reports and plans with metadata (type, dates, has_summary, has_narrative, delivery status). Default limit 10.",
	})

	addStubTool[getReportInput](s, &mcp.Tool{
		Name:        "get_report",
		Description: "Returns the full summary and narrative markdown for a single training report or plan. Lookup by id, by type+week_start, or by latest_type.",
	})
}

// addStubTool registers a tool whose handler returns a not-yet-implemented error.
// WP6 replaces each stub with a real mTLS call to the app's /api/mcp/v1/* endpoint.
func addStubTool[In any](s *mcp.Server, t *mcp.Tool) {
	mcp.AddTool(s, t, func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, any, error) {
		return ToolError("not yet implemented — WP6")
	})
}

// ── Prompt registration (§7) ──────────────────────────────────────────────────

func registerPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "review-recent-training",
		Description: "Analyse a recent training block: what went well, what deviated, key metrics, and risks going forward.",
		Arguments: []*mcp.PromptArgument{
			{Name: "last_days", Description: "Number of days to look back (default 28).", Required: false},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptStub("review-recent-training", "WP7"), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "plan-outdoor-ride",
		Description: "Discuss pacing, fuelling, and climb strategy for an upcoming outdoor ride given the athlete's current fitness.",
		Arguments: []*mcp.PromptArgument{
			{Name: "terrain", Description: "Terrain type or route description.", Required: true},
			{Name: "duration_min", Description: "Expected ride duration in minutes.", Required: true},
			{Name: "date", Description: "Planned date (YYYY-MM-DD).", Required: false},
			{Name: "notes", Description: "Any extra context (weather, goals, concerns).", Required: false},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptStub("plan-outdoor-ride", "WP7"), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "equipment-advice",
		Description: "Get equipment and gear advice grounded in the athlete's profile, goals, and typical terrain.",
		Arguments: []*mcp.PromptArgument{
			{Name: "topic", Description: "Topic to discuss (e.g. tyres, gearing, bike fit, nutrition products).", Required: true},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptStub("equipment-advice", "WP7"), nil
	})

	s.AddPrompt(&mcp.Prompt{
		Name:        "explain-workout",
		Description: "Explain the physiology and coaching intent behind a specific workout.",
		Arguments: []*mcp.PromptArgument{
			{Name: "workout_ref", Description: "Workout ID or date (YYYY-MM-DD) to look up.", Required: true},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptStub("explain-workout", "WP7"), nil
	})
}

// promptStub returns a placeholder prompt result for WP3.
// WP7 replaces each stub with real tool pre-pulling and message construction.
func promptStub(name, wp string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: name,
		Messages: []*mcp.PromptMessage{
			{
				Role:    "user",
				Content: &mcp.TextContent{Text: "Prompt template not yet implemented — " + wp},
			},
		},
	}
}
