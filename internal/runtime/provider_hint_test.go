package runtime

import (
	"strings"
	"testing"

	"github.com/SocialGouv/claw-code-go/internal/api"
	"github.com/SocialGouv/claw-code-go/internal/auth"
)

// A foreign-provider hint is advice a user follows literally, so it is held to
// the machine rather than to itself: the route it names must be the one this
// binary resolves, and the credential it names must be the one that funds it.
//
// Moonshot's hint used to read "set ANTHROPIC_BASE_URL=<moonshot endpoint>".
// That advice is wrong twice over now that Kimi is a first-class provider
// here: ANTHROPIC_BASE_URL is z.ai's own documented wiring knob, so on a host
// carrying both keys it already holds z.ai's endpoint and retargeting it ships
// one vendor's credential to the other's gateway — and even pointed correctly,
// it leaves two providers behind ONE base URL, so every reader downstream that
// maps a route back to the credential that paid has to guess between them.
func TestForeignProviderHint_MoonshotNamesTheRouteThisBinaryResolves(t *testing.T) {
	hint := foreignProviderHint(t, "MOONSHOT_API_KEY")

	// The advice must be executable: the prefix it tells the user to type is
	// the one SelectProvider answers, not a name that merely reads well.
	prefix, _, ok := strings.Cut("moonshot/kimi-k2", "/")
	if !ok {
		t.Fatal("malformed probe spec")
	}
	if got := SelectProvider(prefix).Name(); got != "moonshot" {
		t.Fatalf("SelectProvider(%q).Name() = %q — the hint advertises a prefix this binary does not route", prefix, got)
	}
	if !strings.Contains(hint.Hint, "moonshot/") {
		t.Errorf("hint does not name the `moonshot/` prefix route: %q", hint.Hint)
	}

	// And the key that TRIGGERS the hint must fund that same route, or the
	// user follows working advice into an unauthenticated run.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("MOONSHOT_API_KEY", "moonshot-key")
	provider, token, method, err := auth.ResolveCredentials()
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if provider != "moonshot" || token != "moonshot-key" || method != "api_key" {
		t.Errorf("MOONSHOT_API_KEY resolved to (%q, %q) — the hint's own variable does not fund the route it names", provider, method)
	}

	// The retarget must not come back: it is the instruction that produces the
	// ambiguous configuration, and a hint is the one place a user is told to
	// create it on purpose.
	if strings.Contains(hint.Hint, "ANTHROPIC_BASE_URL=") {
		t.Errorf("hint tells the operator to retarget ANTHROPIC_BASE_URL at Moonshot: %q", hint.Hint)
	}
}

// foreignProviderHint returns the entry for one env var, failing the test when
// the table does not carry it.
func foreignProviderHint(t *testing.T, envVar string) api.ForeignProviderEnvVar {
	t.Helper()
	for _, fp := range api.ForeignProviderEnvVars {
		if fp.EnvVar == envVar {
			return fp
		}
	}
	t.Fatalf("no foreign-provider entry for %s", envVar)
	return api.ForeignProviderEnvVar{}
}
