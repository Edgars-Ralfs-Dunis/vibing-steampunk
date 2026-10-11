package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// fakeReportBridge serves the ZADT_VSP WebSocket: the welcome, then one reply
// per request, whose data is jobStatus for getJobStatus and a scheduled job
// for anything else. Every request the bridge received lands on the channel.
func fakeReportBridge(t *testing.T, jobStatus string) (*Server, chan adt.WSMessage) {
	t.Helper()
	got := make(chan adt.WSMessage, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"id":"welcome","success":true,"data":{"session":"t"}}`))
		for {
			var m adt.WSMessage
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			got <- m
			data := `{"status":"scheduled","report":"ZR","jobname":"ZVSP_ZR","jobcount":"12345678"}`
			if m.Action == "getJobStatus" {
				data = jobStatus
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"id":%q,"success":true,"data":%s}`, m.ID, data)))
		}
	}))
	t.Cleanup(srv.Close)

	s := testServer()
	s.config.BaseURL = srv.URL
	s.adtClient = adt.NewClient(srv.URL, "", "")
	return s, got
}

const finishedJob = `{"jobname":"ZVSP_ZR","jobcount":"12345678","status":"finished","spool_ids":[]}`

func runReport(t *testing.T, s *Server, params string) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := s.handleRunReport(context.Background(), newRequest(map[string]any{"report": "ZR", "params": params}))
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Content[0].(mcp.TextContent).Text
}

// RWF 6: the description's own select-option example used to fail with
// "cannot unmarshal object". A single I/EQ row is what the deployed bridge
// understands, so it goes out as the plain value.
func TestRunReportAcceptsSelectOptionRow(t *testing.T) {
	s, got := fakeReportBridge(t, finishedJob)
	res, text := runReport(t, s, `{"P_BUKRS":"1000","S_KUNNR":{"SIGN":"I","OPTION":"EQ","LOW":"0000001000"},"S_X":["A"]}`)
	if res.IsError {
		t.Fatalf("RunReport refused the select-option row: %s", text)
	}
	sent := (<-got).Params["params"].(map[string]any)
	if sent["S_KUNNR"] != "0000001000" || sent["P_BUKRS"] != "1000" || sent["S_X"] != "A" {
		t.Errorf("the bridge received params %v", sent)
	}
}

// A real range, or a value longer than RSPARAMS-LOW (CHAR45), must be refused
// before anything is scheduled: the deployed bridge drops ranges silently and
// truncates long values, and the report would run anyway.
func TestRunReportRefusesWhatTheBridgeCannotCarry(t *testing.T) {
	for name, params := range map[string]string{
		"between":  `{"S_X":{"SIGN":"I","OPTION":"BT","LOW":"A","HIGH":"B"}}`,
		"exclude":  `{"S_X":{"SIGN":"E","LOW":"A"}}`,
		"two rows": `{"S_X":["A","B"]}`,
		"too long": `{"P_X":"` + strings.Repeat("x", 46) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, got := fakeReportBridge(t, finishedJob)
			res, text := runReport(t, s, params)
			if !res.IsError {
				t.Fatalf("RunReport accepted %s: %s", params, text)
			}
			select {
			case m := <-got:
				t.Errorf("the bridge received %s/%s, want nothing scheduled", m.Domain, m.Action)
			default:
			}
		})
	}
}
