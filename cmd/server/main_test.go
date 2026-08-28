package main

import (
	"os"
	"testing"
)

func TestEnvString(t *testing.T) {
	os.Setenv("TEST_KEY", "custom_val")
	defer os.Unsetenv("TEST_KEY")

	if got := envString([]string{"TEST_KEY"}, "default_val"); got != "custom_val" {
		t.Errorf("expected custom_val, got %s", got)
	}

	if got := envString([]string{"NON_EXISTENT_KEY"}, "default_val"); got != "default_val" {
		t.Errorf("expected default_val, got %s", got)
	}
}

func TestEnvInt(t *testing.T) {
	os.Setenv("TEST_INT", "500")
	defer os.Unsetenv("TEST_INT")

	if got := envInt([]string{"TEST_INT"}, 100); got != 500 {
		t.Errorf("expected 500, got %d", got)
	}

	if got := envInt([]string{"NON_EXISTENT_INT"}, 100); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}

	os.Setenv("TEST_INVALID_INT", "abc")
	defer os.Unsetenv("TEST_INVALID_INT")
	if got := envInt([]string{"TEST_INVALID_INT"}, 100); got != 100 {
		t.Errorf("expected 100 on invalid int, got %d", got)
	}
}

func TestEnvBool(t *testing.T) {
	os.Setenv("TEST_BOOL", "true")
	defer os.Unsetenv("TEST_BOOL")

	if got := envBool([]string{"TEST_BOOL"}, false); got != true {
		t.Errorf("expected true, got %v", got)
	}

	if got := envBool([]string{"NON_EXISTENT_BOOL"}, false); got != false {
		t.Errorf("expected false, got %v", got)
	}
}

func TestGetListenAddr(t *testing.T) {
	// Clean up any pre-existing env vars
	os.Unsetenv("ADDR")
	os.Unsetenv("PORT")
	os.Unsetenv("ERROR_LOGGER_ADDR")
	os.Unsetenv("ERROR_LOGGER_PORT")

	if got := getListenAddr(); got != ":9000" {
		t.Errorf("expected default :9000, got %s", got)
	}

	os.Setenv("PORT", "8080")
	if got := getListenAddr(); got != ":8080" {
		t.Errorf("expected :8080 with PORT=8080, got %s", got)
	}
	os.Unsetenv("PORT")

	os.Setenv("PORT", ":8080")
	if got := getListenAddr(); got != ":8080" {
		t.Errorf("expected :8080 with PORT=:8080, got %s", got)
	}
	os.Unsetenv("PORT")

	os.Setenv("ADDR", "127.0.0.1:8000")
	if got := getListenAddr(); got != "127.0.0.1:8000" {
		t.Errorf("expected 127.0.0.1:8000 with ADDR, got %s", got)
	}
	os.Unsetenv("ADDR")
}
