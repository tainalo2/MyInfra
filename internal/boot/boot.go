package boot

import "os"

// Runtime décrit une copie démarrée sans configuration.
// Close arrête le PostgreSQL que ce processus a lancé. Les fichiers restent.
type Runtime struct {
	DatabaseURL string
	Master      []byte
	Local       bool
	stop        func()
}

func Start() (*Runtime, error) {
	if os.Getenv("DATABASE_URL") != "" {
		return &Runtime{}, nil
	}
	home, err := Home()
	if err != nil {
		return nil, err
	}
	key, err := Master(home)
	if err != nil {
		return nil, err
	}
	url, stop, err := startPostgres(home)
	if err != nil {
		return nil, err
	}
	return &Runtime{DatabaseURL: url, Master: key, Local: true, stop: stop}, nil
}

func (r *Runtime) Close() {
	if r != nil && r.stop != nil {
		r.stop()
	}
}
