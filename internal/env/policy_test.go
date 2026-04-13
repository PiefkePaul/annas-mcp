package env

import "testing"

func TestGetUsagePolicyEnvDefaults(t *testing.T) {
	t.Setenv("ANNAS_OPERATOR_ATTESTS_AUTHORIZED_ACCESS", "")
	t.Setenv("ANNAS_AUTHORIZED_ACCESS_STATEMENT", "")

	got := GetUsagePolicyEnv()
	if got.OperatorAttestsAuthorizedAccess {
		t.Fatal("OperatorAttestsAuthorizedAccess = true, want false")
	}
	if got.Statement() != "" {
		t.Fatalf("Statement() = %q, want empty string", got.Statement())
	}
}

func TestGetUsagePolicyEnvWithDefaultStatement(t *testing.T) {
	t.Setenv("ANNAS_OPERATOR_ATTESTS_AUTHORIZED_ACCESS", "true")
	t.Setenv("ANNAS_AUTHORIZED_ACCESS_STATEMENT", "")

	got := GetUsagePolicyEnv()
	if !got.OperatorAttestsAuthorizedAccess {
		t.Fatal("OperatorAttestsAuthorizedAccess = false, want true")
	}
	if got.Statement() != defaultAuthorizedAccessStatement {
		t.Fatalf("Statement() = %q, want %q", got.Statement(), defaultAuthorizedAccessStatement)
	}
}

func TestGetUsagePolicyEnvWithCustomStatement(t *testing.T) {
	t.Setenv("ANNAS_OPERATOR_ATTESTS_AUTHORIZED_ACCESS", "1")
	t.Setenv("ANNAS_AUTHORIZED_ACCESS_STATEMENT", "Licensed customer archive only.")

	got := GetUsagePolicyEnv()
	if got.Statement() != "Licensed customer archive only." {
		t.Fatalf("Statement() = %q, want %q", got.Statement(), "Licensed customer archive only.")
	}
}
