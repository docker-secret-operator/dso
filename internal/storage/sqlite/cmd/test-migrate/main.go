package main

import (
	"context"
	"fmt"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

func main() {
	fmt.Println("Testing SQLite provider...")

	provider, err := sqlite.NewSQLiteProvider(":memory:")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer provider.Close(context.Background())

	fmt.Println("✅ Success")
}
