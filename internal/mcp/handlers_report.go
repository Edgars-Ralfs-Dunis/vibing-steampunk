// Package mcp provides the MCP server implementation for ABAP ADT tools.
// handlers_report.go contains handlers for report execution and text elements.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// routeReportAction routes "debug" with report-related sub-actions.
func (s *Server) routeReportAction(ctx context.Context, action, objectType, objectName string, params map[string]any) (*mcp.CallToolResult, bool, error) {
	if action != "debug" {
		return nil, false, nil
	}
	switch objectType {
	case "RUN_REPORT":
		return s.callHandler(ctx, s.handleRunReport, params)
	case "RUN_REPORT_ASYNC":
		return s.callHandler(ctx, s.handleRunReportAsync, params)
	case "GET_ASYNC_RESULT":
		return s.callHandler(ctx, s.handleGetAsyncResult, params)
	case "GET_VARIANTS":
		return s.callHandler(ctx, s.handleGetVariants, params)
	case "GET_TEXT_ELEMENTS":
		return s.callHandler(ctx, s.handleGetTextElements, params)
	case "SET_TEXT_ELEMENTS":
		return s.callHandler(ctx, s.handleSetTextElements, params)
	}
	return nil, false, nil
}

// --- Report Execution Handlers ---

func (s *Server) handleRunReport(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Ensure WebSocket is connected
	client := bridgeClientArg(request)
	if errResult := s.ensureWSConnectedFor(ctx, "RunReport", client); errResult != nil {
		return errResult, nil
	}
	bridge, err := s.bridgeFor(client)
	if err != nil {
		return newToolResultError(fmt.Sprintf("RunReport: %v", err)), nil
	}

	// Parse parameters
	report, _ := request.GetArguments()["report"].(string)
	if report == "" {
		return newToolResultError("report parameter is required"), nil
	}

	params := adt.RunReportParams{
		Report: report,
	}

	if variant, ok := request.GetArguments()["variant"].(string); ok {
		params.Variant = variant
	}

	if params.Params, err = reportParams(request.GetArguments()["params"]); err != nil {
		return newToolResultError(err.Error()), nil
	}

	// Step 1: Schedule background job via WebSocket
	result, err := bridge.RunReport(ctx, params)
	if err != nil {
		return newToolResultError(fmt.Sprintf("RunReport failed: %v", err)), nil
	}

	// Check if we got job info (new job-based approach)
	if result.JobName == "" || result.JobCount == "" {
		return newToolResultError("RunReport did not return job info - ABAP service may need updating"), nil
	}

	// Step 2: Poll for job completion (max 60 seconds)
	var jobStatus *adt.JobStatusResult
	pollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	for {
		jobStatus, err = bridge.GetJobStatus(pollCtx, result.JobName, result.JobCount)
		if err != nil {
			return newToolResultError(fmt.Sprintf("GetJobStatus failed: %v", err)), nil
		}

		if jobStatus.Status == "finished" || jobStatus.Status == "aborted" {
			break
		}

		select {
		case <-pollCtx.Done():
			return newToolResultError(fmt.Sprintf("Job %s/%s timed out (status: %s)", result.JobName, result.JobCount, jobStatus.Status)), nil
		case <-time.After(500 * time.Millisecond):
			// Continue polling
		}
	}

	// Step 3: Format output
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", s.bridgeAttributionFor(client))
	fmt.Fprintf(&sb, "Report: %s\n", result.Report)
	fmt.Fprintf(&sb, "Job: %s/%s\n", result.JobName, result.JobCount)
	fmt.Fprintf(&sb, "Status: %s\n", jobStatus.Status)
	if jobStatus.Server != "" {
		fmt.Fprintf(&sb, "Server: %s\n", jobStatus.Server)
	}
	if jobStatus.Status == "aborted" {
		if len(jobStatus.JobLog) > 0 {
			fmt.Fprintf(&sb, "Job log:\n  %s\n", strings.Join(jobStatus.JobLog, "\n  "))
		} else {
			fmt.Fprintf(&sb, "The bridge sent no reason: read the job log with BP_JOBLOG_READ (JOBNAME %s, JOBCOUNT %s).\n", result.JobName, result.JobCount)
		}
	}
	sb.WriteString("\n")

	// Step 4: Get spool output if available
	if len(jobStatus.SpoolIDs) > 0 {
		sb.WriteString("Spool Output:\n")
		for _, spoolID := range jobStatus.SpoolIDs {
			spoolResult, err := bridge.GetSpoolOutput(ctx, spoolID)
			if err != nil {
				fmt.Fprintf(&sb, "  [Spool %s: error reading - %v]\n", spoolID, err)
				continue
			}
			fmt.Fprintf(&sb, "--- Spool %s (%d lines) ---\n", spoolID, spoolResult.Lines)
			sb.WriteString(spoolResult.Output)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("No spool output produced.\n")
	}

	res := mcp.NewToolResultText(sb.String())
	res.IsError = jobStatus.Status == "aborted"
	return res, nil
}

// reportParams reads RunReport's params, a JSON object (or its text) whose
// values are a PARAMETERS value or a select-option row: a string, an object
// {"SIGN","OPTION","LOW","HIGH"}, or an array of those.
//
// ponytail: the deployed ZADT_VSP report service turns each value into one I/EQ
// row and drops anything else without a word, so the report would run
// unrestricted. Only that shape is sent; a real range is refused until the
// service parses typed rows (RWF 6, needs a supervised ABAP redeploy).
func reportParams(raw any) (map[string]string, error) {
	if text, ok := raw.(string); ok {
		if text == "" {
			return nil, nil
		}
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			return nil, fmt.Errorf("Invalid params JSON: %v", err)
		}
	}
	if raw == nil {
		return nil, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("params must be a JSON object keyed by selection-screen name")
	}
	out := make(map[string]string, len(obj))
	for name, v := range obj {
		if rows, ok := v.([]any); ok && len(rows) == 1 {
			v = rows[0]
		}
		var low string
		switch row := v.(type) {
		case string:
			low = row
		case map[string]any:
			fields := map[string]string{}
			for k, f := range row {
				k = strings.ToUpper(k)
				fs, ok := f.(string)
				if !ok || (k != "SIGN" && k != "OPTION" && k != "LOW" && k != "HIGH") {
					return nil, fmt.Errorf("params %s: a row takes the strings SIGN, OPTION, LOW and HIGH, got %s", name, k)
				}
				fields[k] = fs
			}
			if fields["SIGN"] != "" && fields["SIGN"] != "I" || fields["OPTION"] != "" && fields["OPTION"] != "EQ" || fields["HIGH"] != "" {
				return nil, fmt.Errorf("params %s: only a single I/EQ value can be passed until the ZADT_VSP report service is redeployed; the deployed bridge drops ranges (BT, E, HIGH, patterns) and would run the report without them", name)
			}
			low = fields["LOW"]
		case []any:
			return nil, fmt.Errorf("params %s: only a single I/EQ value can be passed until the ZADT_VSP report service is redeployed; the deployed bridge keeps one row per select-option and would drop the rest", name)
		default:
			return nil, fmt.Errorf("params %s: a value must be a string, a {SIGN,OPTION,LOW,HIGH} row or an array of rows", name)
		}
		if utf8.RuneCountInString(name) > 8 {
			return nil, fmt.Errorf("params %s: a selection-screen name has at most 8 characters (RSPARAMS-SELNAME) and SAP would cut it", name)
		}
		// The deployed bridge reads params with a regex and counts braces without
		// skipping strings: JSON escapes arrive as escape text and a brace ends
		// the object early, so the report would run with a changed or dropped row.
		if strings.ContainsAny(name+low, "\"\\<>&{}\u2028\u2029") || strings.IndexFunc(name+low, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("params %s: the deployed ZADT_VSP bridge would change a name or value holding \" \\ < > & { } or a control character; use a variant until the report service is redeployed", name)
		}
		if utf8.RuneCountInString(low) > 45 {
			return nil, fmt.Errorf("params %s: value is %d characters, a selection-screen value holds at most 45 (RSPARAMS-LOW) and SAP would cut it", name, utf8.RuneCountInString(low))
		}
		out[name] = low
	}
	return out, nil
}

func (s *Server) handleRunReportAsync(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Ensure WebSocket is connected
	client := bridgeClientArg(request)
	if errResult := s.ensureWSConnectedFor(ctx, "RunReportAsync", client); errResult != nil {
		return errResult, nil
	}
	bridge, err := s.bridgeFor(client)
	if err != nil {
		return newToolResultError(fmt.Sprintf("RunReportAsync: %v", err)), nil
	}

	// Parse parameters
	report, _ := request.GetArguments()["report"].(string)
	if report == "" {
		return newToolResultError("report parameter is required"), nil
	}

	params := adt.RunReportParams{
		Report: report,
	}

	if variant, ok := request.GetArguments()["variant"].(string); ok {
		params.Variant = variant
	}

	if params.Params, err = reportParams(request.GetArguments()["params"]); err != nil {
		return newToolResultError(err.Error()), nil
	}

	// Generate task ID
	s.asyncTasksMu.Lock()
	s.asyncTaskID++
	taskID := fmt.Sprintf("report_%d_%d", time.Now().Unix(), s.asyncTaskID)
	task := &AsyncTask{
		ID:        taskID,
		Type:      "report",
		Status:    "running",
		StartedAt: time.Now(),
	}
	s.asyncTasks[taskID] = task
	s.asyncTasksMu.Unlock()

	// Run report in background goroutine via WebSocket (job-based)
	go func() {
		bgCtx := context.Background()

		// Step 1: Schedule background job
		result, err := bridge.RunReport(bgCtx, params)
		if err != nil {
			s.asyncTasksMu.Lock()
			now := time.Now()
			task.EndedAt = &now
			task.Status = "error"
			task.Error = err.Error()
			s.asyncTasksMu.Unlock()
			return
		}

		if result.JobName == "" || result.JobCount == "" {
			s.asyncTasksMu.Lock()
			now := time.Now()
			task.EndedAt = &now
			task.Status = "error"
			task.Error = "RunReport did not return job info"
			s.asyncTasksMu.Unlock()
			return
		}

		// Step 2: Poll for job completion (max 5 minutes for async)
		pollCtx, cancel := context.WithTimeout(bgCtx, 5*time.Minute)
		defer cancel()

		var jobStatus *adt.JobStatusResult
		for {
			jobStatus, err = bridge.GetJobStatus(pollCtx, result.JobName, result.JobCount)
			if err != nil {
				s.asyncTasksMu.Lock()
				now := time.Now()
				task.EndedAt = &now
				task.Status = "error"
				task.Error = fmt.Sprintf("GetJobStatus failed: %v", err)
				s.asyncTasksMu.Unlock()
				return
			}

			if jobStatus.Status == "finished" || jobStatus.Status == "aborted" {
				break
			}

			select {
			case <-pollCtx.Done():
				s.asyncTasksMu.Lock()
				now := time.Now()
				task.EndedAt = &now
				task.Status = "error"
				task.Error = fmt.Sprintf("Job %s/%s timed out", result.JobName, result.JobCount)
				s.asyncTasksMu.Unlock()
				return
			case <-time.After(1 * time.Second):
				// Continue polling
			}
		}

		// Step 3: Collect spool output
		var spoolOutput strings.Builder
		if len(jobStatus.SpoolIDs) > 0 {
			for _, spoolID := range jobStatus.SpoolIDs {
				spoolResult, err := bridge.GetSpoolOutput(bgCtx, spoolID)
				if err != nil {
					fmt.Fprintf(&spoolOutput, "[Spool %s: error - %v]\n", spoolID, err)
					continue
				}
				fmt.Fprintf(&spoolOutput, "--- Spool %s (%d lines) ---\n", spoolID, spoolResult.Lines)
				spoolOutput.WriteString(spoolResult.Output)
				spoolOutput.WriteString("\n")
			}
		}

		// Mark complete
		s.asyncTasksMu.Lock()
		now := time.Now()
		task.EndedAt = &now
		task.Status = "completed"
		task.Result = map[string]interface{}{
			"report":       result.Report,
			"jobname":      result.JobName,
			"jobcount":     result.JobCount,
			"job_status":   jobStatus.Status,
			"spool_ids":    jobStatus.SpoolIDs,
			"spool_output": spoolOutput.String(),
			"joblog":       jobStatus.JobLog,
			"server":       jobStatus.Server,
		}
		s.asyncTasksMu.Unlock()
	}()

	// Return task ID immediately
	output := map[string]string{
		"task_id": taskID,
		"status":  "started",
		"message": "Report execution started in background. Use GetAsyncResult to check status.",
	}
	outputJSON, _ := json.MarshalIndent(output, "", "  ")
	return mcp.NewToolResultText(string(outputJSON)), nil
}

func (s *Server) handleGetAsyncResult(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := request.GetArguments()["task_id"].(string)
	if taskID == "" {
		return newToolResultError("task_id parameter is required"), nil
	}

	wait, _ := request.GetArguments()["wait"].(bool)

	if wait {
		// Block until complete or timeout
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			s.asyncTasksMu.RLock()
			task, exists := s.asyncTasks[taskID]
			if !exists {
				s.asyncTasksMu.RUnlock()
				return newToolResultError(fmt.Sprintf("Task not found: %s", taskID)), nil
			}
			status := task.Status
			s.asyncTasksMu.RUnlock()

			if status != "running" {
				break
			}

			select {
			case <-timeout:
				return newToolResultError("Timeout waiting for task completion"), nil
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return newToolResultError("Request cancelled"), nil
			}
		}
	}

	// Get task status
	s.asyncTasksMu.RLock()
	task, exists := s.asyncTasks[taskID]
	if !exists {
		s.asyncTasksMu.RUnlock()
		return newToolResultError(fmt.Sprintf("Task not found: %s", taskID)), nil
	}
	// Make a copy for safe access
	taskCopy := *task
	s.asyncTasksMu.RUnlock()

	// Format output
	output, _ := json.MarshalIndent(taskCopy, "", "  ")
	return mcp.NewToolResultText(string(output)), nil
}

func (s *Server) handleGetVariants(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client := bridgeClientArg(request)
	if errResult := s.ensureWSConnectedFor(ctx, "GetVariants", client); errResult != nil {
		return errResult, nil
	}
	bridge, err := s.bridgeFor(client)
	if err != nil {
		return newToolResultError(fmt.Sprintf("GetVariants: %v", err)), nil
	}

	report, _ := request.GetArguments()["report"].(string)
	if report == "" {
		return newToolResultError("report parameter is required"), nil
	}

	result, err := bridge.GetVariants(ctx, report)
	if err != nil {
		return newToolResultError(fmt.Sprintf("GetVariants failed: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", s.bridgeAttributionFor(client))
	fmt.Fprintf(&sb, "Variants for %s:\n\n", result.Report)

	if len(result.Variants) == 0 {
		sb.WriteString("No variants found.\n")
	} else {
		for _, v := range result.Variants {
			if v.Protected {
				fmt.Fprintf(&sb, "  %s (protected)\n", v.Name)
			} else {
				fmt.Fprintf(&sb, "  %s\n", v.Name)
			}
		}
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleGetTextElements(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if errResult := s.ensureWSConnected(ctx, "GetTextElements"); errResult != nil {
		return errResult, nil
	}

	program, _ := request.GetArguments()["program"].(string)
	if program == "" {
		return newToolResultError("program parameter is required"), nil
	}

	language, _ := request.GetArguments()["language"].(string)

	result, err := s.amdpWSClient.GetTextElements(ctx, program, language)
	if err != nil {
		return newToolResultError(fmt.Sprintf("GetTextElements failed: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Text Elements for %s (Language: %s)\n\n", result.Program, result.Language)

	sb.WriteString("Selection Texts:\n")
	if len(result.SelectionTexts) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for key, text := range result.SelectionTexts {
			fmt.Fprintf(&sb, "  %s: %s\n", key, text)
		}
	}
	sb.WriteString("\n")

	sb.WriteString("Text Symbols:\n")
	if len(result.TextSymbols) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for key, text := range result.TextSymbols {
			fmt.Fprintf(&sb, "  TEXT-%s: %s\n", key, text)
		}
	}

	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleSetTextElements(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if errResult := s.ensureWSConnected(ctx, "SetTextElements"); errResult != nil {
		return errResult, nil
	}

	program, _ := request.GetArguments()["program"].(string)
	if program == "" {
		return newToolResultError("program parameter is required"), nil
	}

	params := adt.SetTextElementsParams{
		Program: program,
	}

	if language, ok := request.GetArguments()["language"].(string); ok {
		params.Language = language
	}

	if selTextsStr, ok := request.GetArguments()["selection_texts"].(string); ok && selTextsStr != "" {
		var selTexts map[string]string
		if err := json.Unmarshal([]byte(selTextsStr), &selTexts); err != nil {
			return newToolResultError(fmt.Sprintf("Invalid selection_texts JSON: %v", err)), nil
		}
		params.SelectionTexts = selTexts
	}

	if textSymsStr, ok := request.GetArguments()["text_symbols"].(string); ok && textSymsStr != "" {
		var textSyms map[string]string
		if err := json.Unmarshal([]byte(textSymsStr), &textSyms); err != nil {
			return newToolResultError(fmt.Sprintf("Invalid text_symbols JSON: %v", err)), nil
		}
		params.TextSymbols = textSyms
	}

	if headTextsStr, ok := request.GetArguments()["heading_texts"].(string); ok && headTextsStr != "" {
		var headTexts map[string]string
		if err := json.Unmarshal([]byte(headTextsStr), &headTexts); err != nil {
			return newToolResultError(fmt.Sprintf("Invalid heading_texts JSON: %v", err)), nil
		}
		params.HeadingTexts = headTexts
	}

	if params.SelectionTexts == nil && params.TextSymbols == nil && params.HeadingTexts == nil {
		return newToolResultError("At least one of selection_texts, text_symbols, or heading_texts is required"), nil
	}

	result, err := s.amdpWSClient.SetTextElements(ctx, params)
	if err != nil {
		return newToolResultError(fmt.Sprintf("SetTextElements failed: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Text Elements Updated for %s (Language: %s)\n\n", result.Program, result.Language)
	fmt.Fprintf(&sb, "Status: %s\n", result.Status)
	fmt.Fprintf(&sb, "Selection Texts Set: %d\n", result.SelectionTextsSet)
	fmt.Fprintf(&sb, "Text Symbols Set: %d\n", result.TextSymbolsSet)
	fmt.Fprintf(&sb, "Heading Texts Set: %d\n", result.HeadingTextsSet)

	return mcp.NewToolResultText(sb.String()), nil
}
