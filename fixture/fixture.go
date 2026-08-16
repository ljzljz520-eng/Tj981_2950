package fixture

import (
	_ "embed"
	"fmt"
	"os"

	"drivemigrate/migration"
	"gopkg.in/yaml.v3"
)

//go:embed default.yaml
var defaultFixture []byte

func Load(path string) (migration.SourceDataset, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return migration.SourceDataset{}, fmt.Errorf("read fixture: %w", err)
	}
	return decode(content)
}

func LoadDefault() (migration.SourceDataset, error) {
	return decode(defaultFixture)
}

func decode(content []byte) (migration.SourceDataset, error) {
	var dataset migration.SourceDataset
	if err := yaml.Unmarshal(content, &dataset); err != nil {
		return migration.SourceDataset{}, fmt.Errorf("decode fixture: %w", err)
	}
	if len(dataset.Students) == 0 {
		return migration.SourceDataset{}, fmt.Errorf("fixture has no students")
	}
	return dataset, nil
}
