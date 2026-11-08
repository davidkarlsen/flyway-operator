/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

// The fixtures in testdata/flyway are the merged stdout and stderr of docker.io/flyway/flyway:13 (13.12.1) run with
// -outputType=json against SQLite (postgres-*.log: PostgreSQL 17; conn-error.log: an unreachable PostgreSQL), as a pod log holds them.
// default-run-with-log-lines.log adds log lines around default-run.log, among them JSON lines with an operation, an
// error or individualResults that are not Flyway results; *-v1-to-v2.log run on a database at version 1 with
// version 2 pending; text-output.log is a run without -outputType=json.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "flyway", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseFlywayOutput(t *testing.T) {
	tests := []struct {
		fixture     string
		wantParsed  bool
		wantVersion string // "" means no version reported
		wantError   string // substring of the error message; "" means none
	}{
		// info migrate info on an empty database: the composite's own schemaVersion (1) is not the database's
		{fixture: "default-run.log", wantParsed: true, wantVersion: "2"},
		{fixture: "default-run-with-log-lines.log", wantParsed: true, wantVersion: "2"},
		// info migrate info and migrate on a database at 1 with 2 pending: the trailing info, and targetSchemaVersion
		{fixture: "default-run-v1-to-v2.log", wantParsed: true, wantVersion: "2"},
		{fixture: "migrate-v1-to-v2.log", wantParsed: true, wantVersion: "2"},
		// migrate on an empty database: targetSchemaVersion
		{fixture: "first-migrate.log", wantParsed: true, wantVersion: "2"},
		// migrate with nothing pending: targetSchemaVersion is null, initialSchemaVersion holds the version
		{fixture: "migrate-nothing-pending.log", wantParsed: true, wantVersion: "2"},
		{fixture: "info-only.log", wantParsed: true, wantVersion: "2"},
		// info on an empty database: schemaVersion is null
		{fixture: "info-empty.log", wantParsed: true},
		{fixture: "validate.log", wantParsed: true},
		// repair info: the info result holds the version
		{fixture: "repair-info.log", wantParsed: true, wantVersion: "2"},
		{fixture: "migrate-error.log", wantParsed: true, wantVersion: "2",
			wantError: `Failed to execute script V3__bad.sql SQL State : null Error Code : 1 Message : [SQLITE_ERROR] SQL error or missing database (near "tabl": syntax error) Location : /sql/V3__bad.sql (/sql/V3__bad.sql) Line : 1`},
		{fixture: "postgres-migrate-error.log", wantParsed: true,
			wantError: `Failed to execute script V1__broken.sql SQL State : 42601 Error Code : 0 Message : ERROR: syntax error at or near "THIS" Position: 1 Location : /sql/V1__broken.sql (/sql/V1__broken.sql) Line : 2`},
		{fixture: "conn-error.log", wantParsed: true, wantError: "Unable to obtain connection from database (jdbc:postgresql:********) for user 'u'"},
		{fixture: "text-output.log"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			res, ok := parseFlywayOutput(readFixture(t, tt.fixture))
			if ok != tt.wantParsed {
				t.Fatalf("parsed = %v, want %v", ok, tt.wantParsed)
			}
			if !ok {
				return
			}
			version, hasVersion := flywaySchemaVersion(res)
			if version != tt.wantVersion || hasVersion != (tt.wantVersion != "") {
				t.Errorf("schema version = %q, %v; want %q", version, hasVersion, tt.wantVersion)
			}
			msg := flywayErrorMessage(res)
			if (tt.wantError == "") != (msg == "") || !strings.Contains(msg, tt.wantError) {
				t.Errorf("error message = %q, want it to contain %q", msg, tt.wantError)
			}
		})
	}
}

func TestFlywayErrorMessageIsCleaned(t *testing.T) {
	res, _ := parseFlywayOutput(readFixture(t, "migrate-error.log"))
	msg := flywayErrorMessage(res)
	for _, unwanted := range []string{"---", "Statement", "\n", "  "} {
		if strings.Contains(msg, unwanted) {
			t.Errorf("message %q contains %q", msg, unwanted)
		}
	}
	res, _ = parseFlywayOutput(readFixture(t, "conn-error.log"))
	if msg := flywayErrorMessage(res); len([]rune(msg)) != maxWaitMessage || !strings.HasSuffix(msg, "…") {
		t.Errorf("message of %d runes not cut at %d: %q", len([]rune(msg)), maxWaitMessage, msg)
	}
}

func TestParseFlywayOutputTruncated(t *testing.T) {
	data := readFixture(t, "default-run.log")
	for _, n := range []int{0, 1, len(data) / 2, len(data) - 3} {
		if res, ok := parseFlywayOutput(data[:n]); ok {
			t.Errorf("log cut at %d of %d bytes parsed: %+v", n, len(data), res)
		}
	}
	// a complete result before a cut one still counts: the cut one is not a result
	single := readFixture(t, "first-migrate.log")
	data = append(append(single, '\n'), readFixture(t, "migrate-error.log")[:500]...)
	res, ok := parseFlywayOutput(data)
	if !ok || flywayErrorMessage(res) != "" {
		t.Errorf("complete result before a cut one: parsed %v, error %q", ok, flywayErrorMessage(res))
	}
}

func TestParseFlywayOutputUsesLastResult(t *testing.T) {
	data := append(append(readFixture(t, "migrate-error.log"), '\n'), readFixture(t, "first-migrate.log")...)
	res, ok := parseFlywayOutput(data)
	if !ok || flywayErrorMessage(res) != "" {
		t.Fatalf("parsed %v, error %q: the last result has no error", ok, flywayErrorMessage(res))
	}
}

func TestFlywaySchemaVersionLimits(t *testing.T) {
	for name, tt := range map[string]struct{ info, want string }{
		// multibyte: the limit counts characters, as the CRD's maxLength does
		"64 characters": {info: strings.Repeat("é", maxSchemaVersion), want: strings.Repeat("é", maxSchemaVersion)},
		"65 characters": {info: strings.Repeat("é", maxSchemaVersion+1), want: "3"},
		"blank":         {info: "  ", want: "3"},
		"padded":        {info: " 4 ", want: "4"},
	} {
		t.Run(name, func(t *testing.T) {
			res, ok := parseFlywayOutput([]byte(`{"individualResults":[` +
				`{"operation":"migrate","flywayVersion":"13.12.1","targetSchemaVersion":"3"},` +
				`{"operation":"info","flywayVersion":"13.12.1","schemaVersion":"` + tt.info + `"}],"schemaVersion":1}`))
			if !ok {
				t.Fatal("not parsed")
			}
			if v, _ := flywaySchemaVersion(res); v != tt.want {
				t.Errorf("schema version = %q, want %q", v, tt.want)
			}
		})
	}
}

func TestParseFlywayOutputRejectsOtherJSON(t *testing.T) {
	for _, line := range []string{
		`{"operation":"migrate","targetSchemaVersion":"3"}`, // no flywayVersion
		`{"error":"x"}`,
		`{"error":null}`,
		`{"error":{"stackTrace":"x"}}`,
		`{"individualResults":[]}`,
		`{"individualResults":[{"operation":"info","flywayVersion":"13"},{"ts":1}]}`,
	} {
		if res, ok := parseFlywayOutput([]byte(line)); ok {
			t.Errorf("%s parsed as a result: %+v", line, res)
		}
	}
	for _, line := range []string{
		`{"operation":"info","flywayVersion":"13","schemaVersion":"5"}`,
		`{"error":{"errorCode":"DB_CONNECTION"}}`,
		`{"error":{"message":"m"}}`,
	} {
		if _, ok := parseFlywayOutput([]byte(line)); !ok {
			t.Errorf("%s not parsed as a result", line)
		}
	}
}

// Every line starting with '{' costs a decode that can scan the rest of the log, so the attempts are capped.
func TestParseFlywayOutputCapsDecodeAttempts(t *testing.T) {
	result := readFixture(t, "first-migrate.log")
	junk := strings.Repeat("{\"ts\":1}\n", maxDecodeAttempts-1)
	if _, ok := parseFlywayOutput(append([]byte(junk), result...)); !ok {
		t.Errorf("result after %d other JSON lines not parsed", maxDecodeAttempts-1)
	}
	junk += "{\"ts\":1}\n"
	if _, ok := parseFlywayOutput(append([]byte(junk), result...)); ok {
		t.Errorf("result after %d other JSON lines parsed, want the attempts capped", maxDecodeAttempts)
	}
	// unterminated nested objects on every line: each decode scans to the end of the log
	nested := []byte(strings.Repeat("{\"x\":[\n", 100000))
	start := time.Now()
	if _, ok := parseFlywayOutput(nested); ok {
		t.Error("unterminated JSON parsed")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("parsing %d bytes of nested JSON took %v", len(nested), d)
	}
}

// PodsLogReader asks the API server for at most limitBytes and stops reading there even if more is sent.
func TestPodsLogReaderLimitsTheRead(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/ns/pods/p/log" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.Query()
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	pods, err := corev1client.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	data, err := PodsLogReader{Pods: pods}.ReadLog(context.Background(), "ns", "p", flywayContainerName, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 10 || query.Get("limitBytes") != "10" || query.Get("container") != flywayContainerName {
		t.Errorf("read %d bytes with query %v, want 10 bytes, limitBytes=10, container=%s", len(data), query, flywayContainerName)
	}
}
