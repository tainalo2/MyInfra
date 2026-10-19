//go:build !postgresembed

package boot

func postgresArchive() []byte { return nil }
