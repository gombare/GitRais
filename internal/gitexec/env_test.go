package gitexec

import (
	"strings"
	"testing"
)

// Inherited git configuration injected through the environment is dropped
// along with the repository redirections.
func TestInheritedEnvDropsConfigInjection(t *testing.T) {
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor'='touch /tmp/pwned'")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.pager")
	t.Setenv("GIT_CONFIG_VALUE_0", "evil")
	t.Setenv("GIT_DIR", "/elsewhere")
	t.Setenv("GITRAIS_KEEP", "1")
	env := strings.Join(inheritedEnv(), "\n") + "\n"
	for _, k := range []string{"GIT_CONFIG_PARAMETERS=", "GIT_CONFIG_COUNT=", "GIT_CONFIG_KEY_0=", "GIT_CONFIG_VALUE_0=", "GIT_DIR="} {
		if strings.Contains("\n"+env, "\n"+k) {
			t.Errorf("%s inherited", k)
		}
	}
	if !strings.Contains(env, "GITRAIS_KEEP=1\n") {
		t.Error("unrelated variable dropped")
	}
}

// A parent's credential prompt (an editor's GIT_ASKPASS, say) is dropped: git must
// prompt only through the helper the session sets, or not at all.
func TestInheritedEnvDropsPromptHelpers(t *testing.T) {
	t.Setenv("GIT_ASKPASS", "/editor/askpass.sh")
	t.Setenv("SSH_ASKPASS", "/editor/askpass.sh")
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")
	env := strings.Join(inheritedEnv(), "\n") + "\n"
	for _, k := range []string{"GIT_ASKPASS=", "SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE="} {
		if strings.Contains("\n"+env, "\n"+k) {
			t.Errorf("%s inherited", k)
		}
	}
}
