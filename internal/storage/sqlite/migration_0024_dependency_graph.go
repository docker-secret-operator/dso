package sqlite

var migration0024statements = []string{
	`CREATE TABLE IF NOT EXISTS graph_nodes (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		status TEXT DEFAULT 'healthy',
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_graph_nodes_type ON graph_nodes(type)`,
	`CREATE INDEX IF NOT EXISTS idx_graph_nodes_status ON graph_nodes(status)`,
	`CREATE TABLE IF NOT EXISTS graph_edges (
		id TEXT PRIMARY KEY,
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		type TEXT NOT NULL,
		weight INTEGER DEFAULT 50,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (source_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
		FOREIGN KEY (target_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
		UNIQUE(source_id, target_id, type)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(source_id)`,
	`CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(target_id)`,
	`CREATE INDEX IF NOT EXISTS idx_graph_edges_type ON graph_edges(type)`,
}
