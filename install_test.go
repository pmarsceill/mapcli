package mapcli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestInstallScriptExists(t *testing.T) {
	_, err := os.Stat("install.sh")
	if os.IsNotExist(err) {
		t.Fatal("install.sh does not exist")
	}
	if err != nil {
		t.Fatalf("failed to stat install.sh: %v", err)
	}
}

func TestInstallScriptExecutable(t *testing.T) {
	info, err := os.Stat("install.sh")
	if err != nil {
		t.Fatalf("failed to stat install.sh: %v", err)
	}

	// Check if executable bit is set for owner
	if info.Mode()&0100 == 0 {
		t.Error("install.sh should be executable")
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n", "install.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh has syntax errors: %v\n%s", err, output)
	}
}

func TestInstallScriptContent(t *testing.T) {
	content, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatalf("failed to read install.sh: %v", err)
	}

	script := string(content)

	// Check for required elements
	checks := []struct {
		name     string
		contains string
	}{
		{"shebang", "#!/bin/bash"},
		{"repo reference", "pmarsceill/mapcli"},
		{"OS detection", "uname -s"},
		{"arch detection", "uname -m"},
		{"GitHub API", "api.github.com"},
		{"install dir", "INSTALL_DIR"},
		{"error handling", "set -e"},
		{"linux support", "linux"},
		{"darwin support", "darwin"},
		{"amd64 support", "amd64"},
		{"arm64 support", "arm64"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(script, check.contains) {
				t.Errorf("install.sh should contain %q", check.contains)
			}
		})
	}
}

func TestInstallScriptShellcheck(t *testing.T) {
	// Skip if shellcheck is not installed
	_, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck not installed")
	}

	cmd := exec.Command("shellcheck", "install.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck found issues:\n%s", output)
	}
}
