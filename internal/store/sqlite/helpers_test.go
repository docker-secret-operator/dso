package sqlite

// curV is the schema version of this binary (the number of embedded
// migrations); tests that add a hypothetical next migration use curV+1 so they
// do not break when a real migration is added.
var curV = func() int {
	ms, err := EmbeddedMigrations()
	if err != nil {
		panic(err)
	}
	return len(ms)
}()
