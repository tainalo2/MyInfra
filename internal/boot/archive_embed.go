//go:build postgresembed

package boot

import _ "embed"

//go:embed postgres.tar.gz
var postgresTarGz []byte

func postgresArchive() []byte { return postgresTarGz }
