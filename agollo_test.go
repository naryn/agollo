package agollo

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/philchia/agollo/internal/mockserver"
)

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	go func() {
		if err := mockserver.Run(); err != nil {
			log.Fatal(err)
		}
	}()
	// wait for mock server to run
	time.Sleep(time.Millisecond * 10)
}

func teardown() {
	mockserver.Close()
}

func TestAgolloStart(t *testing.T) {
	if err := Start(); err == nil {
		t.Errorf("Start with default app.properties should return err, got :%v", err)
	}

	if err := StartWithConfFile("fake.properties"); err == nil {
		t.Errorf("Start with fake.properties should return err, got :%v", err)
	}

	if err := StartWithConfFile("./testdata/app.properties"); err != nil {
		t.Errorf("Start with app.properties should return nil, got :%v", err)
	}
	defer Stop()
	defer os.Remove(defaultDumpFile)

	if err := defaultClient.loadLocal(defaultDumpFile); err != nil {
		t.Errorf("loadLocal should return nil, got: %v", err)
	}

	mockserver.Set("application", "key", "value")
	updates := WatchUpdate()

	select {
	case <-updates:
	case <-time.After(time.Millisecond * 30000):
	}

	val := GetStringValue("key", "defaultValue")
	if val != "value" {
		t.Errorf("GetStringValue of key should = value, got %v", val)
	}

	mockserver.Set("application", "key", "newvalue")
	select {
	case <-updates:
	case <-time.After(time.Millisecond * 30000):
	}

	val = defaultClient.GetStringValue("key", "defaultValue")
	if val != "newvalue" {
		t.Errorf("GetStringValue of key should = newvalue, got %v", val)
	}

	mockserver.Delete("application", "key")
	select {
	case <-updates:
	case <-time.After(time.Millisecond * 30000):
	}

	val = GetStringValue("key", "defaultValue")
	if val != "defaultValue" {
		t.Errorf("GetStringValue of key should = defaultValue, got %v", val)
	}

	mockserver.Set("client.json", "content", `{"name":"agollo"}`)
	select {
	case <-updates:
	case <-time.After(time.Millisecond * 30000):
	}

	val = GetNameSpaceContent("client.json", "{}")
	if val != `{"name":"agollo"}` {
		t.Errorf(`GetStringValue of client.json content should  = {"name":"agollo"}, got %v`, val)
	}
}
