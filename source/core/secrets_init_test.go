package core

// Tests never touch the real credential store; secrets_test.go swaps in a map where needed.
func init() { useSecretBackend(nil) }
