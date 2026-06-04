package storage

// PersistenceConfig represents the persistence layer configuration
type PersistenceConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Driver          string `yaml:"driver" json:"driver"`   // sqlite
	Path            string `yaml:"path" json:"path"`       // data/dso.db
	EncryptionKey   string `yaml:"encryption_key" json:"encryption_key,omitempty"` // Sensitive, from env
	MaxConnections  int    `yaml:"max_connections" json:"max_connections"`
	MaxIdleConns    int    `yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
}

// DefaultPersistenceConfig returns default persistence configuration
func DefaultPersistenceConfig() PersistenceConfig {
	return PersistenceConfig{
		Enabled:        false, // Opt-in for backward compatibility
		Driver:         "sqlite",
		Path:           "data/dso.db",
		MaxConnections: 25,
		MaxIdleConns:   5,
		ConnMaxLifetime: "0", // No limit
	}
}
