package cli

import (
	"bytes"
	"testing"
)

func TestVersionDefault(t *testing.T) {
	// Version should have a default value
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestRootCmdHasVersion(t *testing.T) {
	// Root command should have version set
	if rootCmd.Version == "" {
		t.Error("rootCmd.Version should not be empty")
	}

	// Version on rootCmd should match the package Version variable
	if rootCmd.Version != Version {
		t.Errorf("rootCmd.Version (%q) should match Version (%q)", rootCmd.Version, Version)
	}
}

func TestVersionFlag(t *testing.T) {
	// Capture output when running with --version flag
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("--version should produce output")
	}

	// Output should contain the version
	if !containsString(output, Version) {
		t.Errorf("version output %q should contain version %q", output, Version)
	}

	// Reset args for other tests
	rootCmd.SetArgs([]string{})
}

func TestVersionFlagShort(t *testing.T) {
	// Capture output when running with -v flag
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"-v"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("-v should produce output")
	}

	// Reset args for other tests
	rootCmd.SetArgs([]string{})
}
