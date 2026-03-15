package socket

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestClientCall(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, slog.Default())
	srv.Handle("greet", func(params json.RawMessage) (any, error) {
		var p struct{ Name string }
		json.Unmarshal(params, &p)
		return map[string]string{"message": "hello " + p.Name}, nil
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	time.Sleep(10 * time.Millisecond)

	client, err := Dial(dir)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	result, err := client.Call("greet", map[string]string{"Name": "world"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var resp map[string]string
	json.Unmarshal(result, &resp)
	if resp["message"] != "hello world" {
		t.Errorf("message = %q", resp["message"])
	}
}

func TestClientCallError(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, slog.Default())
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	time.Sleep(10 * time.Millisecond)

	client, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.Call("nonexistent", nil)
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestDialNoServer(t *testing.T) {
	_, err := Dial("/tmp/definitely-no-server-here-" + t.Name())
	if err == nil {
		t.Error("expected error when no server running")
	}
}
