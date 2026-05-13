package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func runDispatch(t *testing.T, argv []string, jsonMode bool) (string, string, int) {
	t.Helper()
	// We need temp files because Dispatch takes *os.File.
	outF, _ := os.CreateTemp("", "out*")
	errF, _ := os.CreateTemp("", "err*")
	defer os.Remove(outF.Name())
	defer os.Remove(errF.Name())
	code := Dispatch(context.Background(), demo(), argv, jsonMode, outF, errF)
	outF.Close()
	errF.Close()
	out, _ := os.ReadFile(outF.Name())
	errb, _ := os.ReadFile(errF.Name())
	return string(out), string(errb), code
}

func TestEchoHumanOutput(t *testing.T) {
	out, _, code := runDispatch(t, []string{"echo", "hello", "world"}, false)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if strings.TrimSpace(out) != "hello world" {
		t.Fatalf("out=%q", out)
	}
}

func TestEchoJSONOutput(t *testing.T) {
	out, _, code := runDispatch(t, []string{"echo", "hi"}, true)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var env Result
	if err := json.Unmarshal(bytes.TrimSpace([]byte(out)), &env); err != nil {
		t.Fatalf("decode: %v: %s", err, out)
	}
	if !env.OK || env.Data != "hi" {
		t.Fatalf("env=%+v", env)
	}
}

func TestTimeUnix(t *testing.T) {
	out, _, code := runDispatch(t, []string{"time", "--format", "unix"}, true)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `"unix":`) {
		t.Fatalf("out=%q", out)
	}
}

func TestHelpListsSubcommands(t *testing.T) {
	out, _, code := runDispatch(t, []string{}, false)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	for _, name := range []string{"echo", "time"} {
		if !strings.Contains(out, name) {
			t.Fatalf("help missing %q in: %s", name, out)
		}
	}
}

func TestJSONHelpEmitsCapabilities(t *testing.T) {
	out, _, code := runDispatch(t, []string{}, true)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var env Result
	if err := json.Unmarshal(bytes.TrimSpace([]byte(out)), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data not a map: %+v", env.Data)
	}
	subs, _ := data["subcommands"].([]any)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subcommands, got %v", subs)
	}
}
