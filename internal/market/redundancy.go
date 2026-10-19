package market

import "strings"

// Kind says how a service keeps its data, and so how it can be made redundant.
type Kind string

const (
	// Stateless runs as many copies as wanted.
	Stateless Kind = "stateless"
	// Database keeps its data in PostgreSQL and its files in object storage
	// when told to: then it runs as many copies as wanted.
	Database Kind = "database"
	// SingleWriter keeps its data in SQLite files: one copy runs, the files are
	// replicated, and the copy starts again on another node if its own fails.
	SingleWriter Kind = "single"
	// Unknown keeps files MyInfra knows nothing about: it stays on its disk.
	Unknown Kind = "unknown"
)

// Profile is what MyInfra knows of an image.
type Profile struct {
	Kind Kind
	// SQLite lists the database files of a single writer.
	SQLite []string
	// Env pins what a backed-up single writer must keep, for instance its
	// database engine, so that it cannot choose one the backup does not copy.
	Env map[string]string
	// Postgres maps the variables that point a database service to
	// PostgreSQL and its files to object storage.
	Postgres *PostgresEnv
}

// PostgresEnv names the variables an application reads. Fixed holds values
// that do not depend on the cluster.
type PostgresEnv struct {
	Database string
	Host     string
	User     string
	Password string
	Name     string
	Fixed    map[string]string
	// Files is where the application keeps its files on disk: they move to
	// object storage through the S3 variables below.
	Files       string
	S3Endpoint  string
	S3Bucket    string
	S3Region    string
	S3AccessKey string
	S3SecretKey string
	S3Fixed     map[string]string
	// Dump and Restore move the data of the disk into PostgreSQL and object
	// storage: {dir} is a folder on the service's disk.
	Dump    []string
	Restore []string
	// RestoreInput answers the restore's questions, for applications that ask
	// to confirm on the terminal.
	RestoreInput string
	// ToolEnv is set on the dump and the restore, which MyInfra runs from the
	// image mounted elsewhere than where the application expects to live.
	ToolEnv map[string]string
	// Tables must hold rows after the move when the dump has some: the move
	// is checked, not trusted, before the service leaves its disk.
	Tables []string
}

var redundancy = map[string]Profile{
	"louislam/uptime-kuma": {Kind: SingleWriter, SQLite: []string{"/app/data/kuma.db"}, Env: map[string]string{"UPTIME_KUMA_DB_TYPE": "sqlite"}},
	"neosmemo/memos":       {Kind: SingleWriter, SQLite: []string{"/var/opt/memos/memos_prod.db"}},
	"vikunja/vikunja": {Kind: Database, Postgres: &PostgresEnv{
		Database: "vikunja", Host: "VIKUNJA_DATABASE_HOST", User: "VIKUNJA_DATABASE_USER",
		Password: "VIKUNJA_DATABASE_PASSWORD", Name: "VIKUNJA_DATABASE_DATABASE",
		Fixed:      map[string]string{"VIKUNJA_DATABASE_TYPE": "postgres", "VIKUNJA_DATABASE_SSLMODE": "require"},
		Files:      "/app/vikunja/files",
		S3Endpoint: "VIKUNJA_FILES_S3_ENDPOINT", S3Bucket: "VIKUNJA_FILES_S3_BUCKET", S3Region: "VIKUNJA_FILES_S3_REGION",
		S3AccessKey: "VIKUNJA_FILES_S3_ACCESSKEY", S3SecretKey: "VIKUNJA_FILES_S3_SECRETKEY",
		S3Fixed: map[string]string{"VIKUNJA_FILES_TYPE": "s3", "VIKUNJA_FILES_S3_USEPATHSTYLE": "true"},
		Dump:    []string{"/app/vikunja/vikunja", "dump", "-p", "{dir}", "-f", "dump.zip"},
		Restore: []string{"/app/vikunja/vikunja", "restore", "--preserve-config", "{dir}/dump.zip"},
		Tables:  []string{"users", "projects", "tasks", "files"},
		// The restore asks to type this before it wipes the empty database.
		RestoreInput: "Yes, I understand\n",
		// Its files are found from the folder of its program, which moves.
		ToolEnv: map[string]string{"VIKUNJA_SERVICE_ROOTPATH": "/app/vikunja"},
	}},
}

// ProfileOf tells what MyInfra knows of an image. A service without a disk is
// stateless whatever its image.
func ProfileOf(image string, disk bool) Profile {
	if !disk {
		return Profile{Kind: Stateless}
	}
	if profile, ok := redundancy[repository(image)]; ok {
		return profile
	}
	return Profile{Kind: Unknown}
}

// repository strips the registry of Docker Hub, the tag and the digest.
func repository(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	if slash := strings.LastIndex(image, "/"); strings.LastIndex(image, ":") > slash {
		image = image[:strings.LastIndex(image, ":")]
	}
	image = strings.TrimPrefix(image, "docker.io/")
	image = strings.TrimPrefix(image, "index.docker.io/")
	return strings.TrimPrefix(image, "library/")
}
