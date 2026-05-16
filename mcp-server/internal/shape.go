package internal

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolError returns a tool-level error result (isError: true in the MCP response).
// Use this for domain errors (not found, invalid input, app unavailable) so the
// MCP client receives a structured error rather than a protocol-level failure.
func ToolError(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// AppUnavailableError returns a tool error for when the cycling-coach app cannot be reached.
// Maps to the error-app-unavailable fixture in the WP0 contract.
func AppUnavailableError(err error) (*mcp.CallToolResult, any, error) {
	return ToolError(fmt.Sprintf("app unavailable: %v", err))
}

// NotFoundError returns a tool error for 404 responses from the app.
// Maps to the error-not-found fixture in the WP0 contract.
func NotFoundError(what string) (*mcp.CallToolResult, any, error) {
	return ToolError(fmt.Sprintf("%s not found", what))
}
