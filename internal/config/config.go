package config

import "os"

type Config struct {
	ListenAddress string
	DatabasePath  string
	DataDir       string
}

func FromEnv() Config {
	dataDir := getenv("RAT_DATA_DIR", "./data")
	return Config{
		ListenAddress: getenv("RAT_LISTEN_ADDR", ":8080"),
		DatabasePath:  getenv("RAT_DATABASE_PATH", dataDir+"/rat.sqlite"),
		DataDir:       dataDir,
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
