package project

// CorruptKeyedOpsForTest makes the keyed door hand the patcher an op whose
// value is the wrong type, so a caller outside this package — a real
// phase.Plan, saved through SaveTOML — can prove the verify step refuses what
// the patcher got wrong. The returned func restores the real diff.
func CorruptKeyedOpsForTest() (restore func()) {
	orig := keyedOps
	keyedOps = func(old, new any, keys map[string]string) ([]op, error) {
		ops, err := diffKeyed(old, new, keys)
		if err != nil || len(ops) == 0 {
			return ops, err
		}
		ops[0].value = int64(42)
		return ops, nil
	}
	return func() { keyedOps = orig }
}
