package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var decimalPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func manySymbols(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "700.HK"
	}
	return strings.Join(parts, ",")
}

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

func TestQuotesBatchMatchesSingleQuotes(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	var batch []map[string]any
	getJSON(t, ts.URL+"/v1/quotes?symbols=9988.HK,700.HK", &batch)
	if len(batch) != 2 || batch[0]["symbol"] != "9988.HK" || batch[1]["symbol"] != "700.HK" {
		t.Fatalf("batch = %v", batch)
	}
	var single map[string]any
	getJSON(t, ts.URL+"/v1/quotes/700.HK", &single)
	for _, k := range []string{"price", "change", "changePercent", "currency", "asOf"} {
		if batch[1][k] != single[k] {
			t.Errorf("%s: batch %v != single %v", k, batch[1][k], single[k])
		}
	}
}

func TestWatchlistsShape(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	var lists []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Symbols []struct{ Symbol, Name string }
	}
	getJSON(t, ts.URL+"/v1/watchlists", &lists)
	if len(lists) == 0 || lists[0].ID == "" || len(lists[0].Symbols) == 0 || lists[0].Symbols[0].Name == "" {
		t.Fatalf("watchlists = %+v", lists)
	}
}

func TestCapitalFlowShape(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	var cf struct {
		Symbol   string `json:"symbol"`
		Currency string `json:"currency"`
		AsOf     string `json:"asOf"`
		Flow     []struct {
			Time   string `json:"time"`
			Inflow string `json:"inflow"`
		} `json:"flow"`
		Distribution map[string]map[string]string `json:"distribution"`
	}
	getJSON(t, ts.URL+"/v1/capital-flow/700.HK", &cf)
	if cf.Symbol != "700.HK" || cf.Currency != "HKD" || cf.AsOf == "" || len(cf.Flow) == 0 {
		t.Fatalf("capital flow = %+v", cf)
	}
	if !decimalPattern.MatchString(cf.Flow[0].Inflow) || !strings.HasSuffix(cf.Flow[0].Time, "Z") {
		t.Errorf("flow point = %+v", cf.Flow[0])
	}
	for _, side := range []string{"in", "out", "net"} {
		for _, bucket := range []string{"large", "medium", "small"} {
			if v, ok := cf.Distribution[side][bucket]; !ok || !decimalPattern.MatchString(v) {
				t.Errorf("distribution.%s.%s = %q", side, bucket, v)
			}
		}
	}
	// net is computed server side: in - out
	if cf.Distribution["net"]["large"] == cf.Distribution["in"]["large"] {
		t.Error("net.large equals in.large; expected in minus out")
	}
}

func TestIntradayShape(t *testing.T) {
	ts := newTestServer() // mock clock is 2026-09-17 00:00Z, before the open: points must be [] not null
	defer ts.Close()
	var in struct {
		Symbol    string `json:"symbol"`
		Currency  string `json:"currency"`
		PrevClose string `json:"prevClose"`
		Points    []struct {
			Time, Price, AvgPrice string
			Volume                int64
		} `json:"points"`
	}
	getJSON(t, ts.URL+"/v1/intraday/700.HK", &in)
	if in.Symbol != "700.HK" || in.Currency != "HKD" || !decimalPattern.MatchString(in.PrevClose) {
		t.Fatalf("header: %+v", in)
	}
	if in.Points == nil {
		t.Fatal("points must serialize as [] before the open")
	}
	resp, err := http.Get(ts.URL + "/v1/intraday/700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	if string(raw["points"]) != "[]" {
		t.Errorf("points json = %s, want []", raw["points"])
	}
}
