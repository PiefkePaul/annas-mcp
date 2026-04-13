package env

import (
	"os"
	"strconv"
	"strings"
)

const defaultAuthorizedAccessStatement = "The server operator attests that this MCP server is restricted to rights-cleared, pre-authorized access only. Each user is expected to access only files they are already licensed, permitted, or otherwise entitled to download."

type UsagePolicyEnv struct {
	OperatorAttestsAuthorizedAccess bool   `json:"operator_attests_authorized_access"`
	AuthorizedAccessStatement       string `json:"authorized_access_statement,omitempty"`
}

func GetUsagePolicyEnv() *UsagePolicyEnv {
	return &UsagePolicyEnv{
		OperatorAttestsAuthorizedAccess: getBoolEnv("ANNAS_OPERATOR_ATTESTS_AUTHORIZED_ACCESS"),
		AuthorizedAccessStatement:       strings.TrimSpace(os.Getenv("ANNAS_AUTHORIZED_ACCESS_STATEMENT")),
	}
}

func (e *UsagePolicyEnv) Statement() string {
	if e == nil || !e.OperatorAttestsAuthorizedAccess {
		return ""
	}
	if strings.TrimSpace(e.AuthorizedAccessStatement) != "" {
		return strings.TrimSpace(e.AuthorizedAccessStatement)
	}
	return defaultAuthorizedAccessStatement
}

func getBoolEnv(key string) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}
