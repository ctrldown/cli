package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

const (
	CurrentSchemaVersion = 2
	CurrentCLIVersion    = "0.2.3"
)

// Build-time variables overridden via -ldflags during release builds.
var (
	Version    = CurrentCLIVersion
	Commit     = "dev"
	BuildDate  = "unknown"
	GitHubRepo = "ctrldown/cli"
)

type CtrlConfig struct {
	SchemaVersion   int                    `mapstructure:"schema_version" yaml:"schema_version"`
	CLIVersion      string                 `mapstructure:"cli_version" yaml:"cli_version,omitempty"`
	BoilerplatePath string                 `mapstructure:"boilerplate_path" yaml:"boilerplate_path,omitempty"`
	LibraryPath     string                 `mapstructure:"library_path" yaml:"library_path,omitempty"`
	Project         ProjectConfig          `mapstructure:"project" yaml:"project"`
	Backend         *BackendConfig         `mapstructure:"backend" yaml:"backend,omitempty"`
	Domains         []DomainEntry          `mapstructure:"domains" yaml:"domains,omitempty"`
	Packages        []PackageEntry         `mapstructure:"packages" yaml:"packages,omitempty"`
	Features        []string               `mapstructure:"features" yaml:"features,omitempty"`
	FeatureLocks    map[string]FeatureLock `mapstructure:"feature_locks" yaml:"feature_locks,omitempty"`
}

type FeatureLock struct {
	Version   string `mapstructure:"version" yaml:"version"`
	Checksum  string `mapstructure:"checksum" yaml:"checksum"`
	UpdatedAt string `mapstructure:"updated_at" yaml:"updated_at"`
}

type ProjectConfig struct {
	Variant      string `mapstructure:"variant" yaml:"variant"`
	Title        string `mapstructure:"title" yaml:"title"`
	Architecture string `mapstructure:"architecture" yaml:"architecture,omitempty"`
	CreatedAt    string `mapstructure:"created_at" yaml:"created_at"`
}

type BackendConfig struct {
	Framework string   `mapstructure:"framework" yaml:"framework"`
	Path      string   `mapstructure:"path" yaml:"path"`
	Module    string   `mapstructure:"module" yaml:"module"`
	Database  string   `mapstructure:"database" yaml:"database,omitempty"`
	Auth      string   `mapstructure:"auth" yaml:"auth,omitempty"`
	Modules   []string `mapstructure:"modules" yaml:"modules,omitempty"`
}

type DomainEntry struct {
	Name string `mapstructure:"name" yaml:"name"`
	Type string `mapstructure:"type" yaml:"type"`
	Path string `mapstructure:"path" yaml:"path"`
}

type PackageEntry struct {
	Name string `mapstructure:"name" yaml:"name"`
	Path string `mapstructure:"path" yaml:"path"`
}

type TemplateDef struct {
	ID            string   `mapstructure:"id" json:"id"`
	Path          string   `mapstructure:"path" json:"path"`
	Title         string   `mapstructure:"title" json:"title"`
	Description   string   `mapstructure:"description" json:"description"`
	Language      string   `mapstructure:"language" json:"language"`
	Tier          string   `mapstructure:"tier" json:"tier"`
	AllowedMixins []string `mapstructure:"allowed_mixins" json:"allowed_mixins"`
}

type MixinDef struct {
	ID          string `mapstructure:"id" json:"id"`
	Path        string `mapstructure:"path" json:"path"`
	Title       string `mapstructure:"title" json:"title"`
	Description string `mapstructure:"description" json:"description"`
	Language    string `mapstructure:"language" json:"language"`
	Group       string `mapstructure:"group" json:"group"`
}

type BoilerplateConfig struct {
	Environment map[string]string                 `mapstructure:"environment" json:"environment"`
	Hooks       map[string][]string               `mapstructure:"hooks" json:"hooks"`
	Templates   map[string]map[string]TemplateDef `mapstructure:"-" json:"-"`
	Mixins      map[string]map[string]MixinDef    `mapstructure:"-" json:"-"`
}

// registrySchema mirrors the compiled registry.json format in boilerplates.
type registrySchema struct {
	Environment map[string]string      `json:"environment"`
	Hooks       map[string][]string    `json:"hooks"`
	Templates   map[string]TemplateDef `json:"templates"`
	Mixins      map[string]MixinDef    `json:"mixins"`
}

func LoadCtrlConfig(fs afero.Fs, workDir string) (*CtrlConfig, error) {
	configPath := filepath.Join(workDir, "ctrl.yaml")
	data, err := afero.ReadFile(fs, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ctrl.yaml (are you in a ctrldown initialized project?): %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to parse ctrl.yaml: %w", err)
	}

	var conf CtrlConfig
	if err := v.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ctrl.yaml: %w", err)
	}
	return &conf, nil
}

// SaveCtrlConfig writes the CtrlConfig struct into ctrl.yaml with a descriptive header.
func SaveCtrlConfig(fs afero.Fs, workDir string, conf *CtrlConfig) error {
	if err := fs.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", workDir, err)
	}

	if conf.SchemaVersion == 0 {
		conf.SchemaVersion = CurrentSchemaVersion
	}
	if conf.CLIVersion == "" {
		conf.CLIVersion = CurrentCLIVersion
	}

	yamlBytes, err := yaml.Marshal(conf)
	if err != nil {
		return fmt.Errorf("failed to marshal ctrl.yaml: %w", err)
	}

	header := `# ========================================================
# CTRLDOWN CONFIGURATION FILE
# ========================================================
# This file contains the project context and registered modules for ctrldown.

`
	fullContent := append([]byte(header), yamlBytes...)
	targetFile := filepath.Join(workDir, "ctrl.yaml")
	if err := afero.WriteFile(fs, targetFile, fullContent, 0644); err != nil {
		return fmt.Errorf("failed to write ctrl.yaml: %w", err)
	}
	return nil
}

func LoadBoilerplateConfig(fs afero.Fs, boilerplatePath string) (*BoilerplateConfig, error) {
	// 1. Try reading registry.json (decentralized boilerplate registry format)
	registryPath := filepath.Join(boilerplatePath, "registry.json")
	if exists, _ := afero.Exists(fs, registryPath); exists {
		return loadFromRegistryJSON(fs, registryPath)
	}

	// 2. Fallback to legacy cli.toml format
	tomlPath := filepath.Join(boilerplatePath, "cli.toml")
	tomlBytes, err := afero.ReadFile(fs, tomlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry.json or cli.toml in %s: %w", boilerplatePath, err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(tomlBytes)); err != nil {
		return nil, fmt.Errorf("failed to parse cli.toml in %s: %w", boilerplatePath, err)
	}

	var conf BoilerplateConfig
	if err := v.Unmarshal(&conf); err != nil {
		return nil, err
	}

	// Parse templates manually to avoid mapstructure panic on scalar "path" vs map categories
	conf.Templates = make(map[string]map[string]TemplateDef)
	if templatesMap := v.GetStringMap("templates"); templatesMap != nil {
		for category, val := range templatesMap {
			if category == "path" {
				continue // Skip the scalar path property
			}
			subMap, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			conf.Templates[category] = make(map[string]TemplateDef)
			for tplName, tplVal := range subMap {
				tplFields, ok := tplVal.(map[string]interface{})
				if !ok {
					continue
				}

				getString := func(key string) string {
					if v, exists := tplFields[key]; exists {
						if str, isStr := v.(string); isStr {
							return str
						}
					}
					return ""
				}

				conf.Templates[category][tplName] = TemplateDef{
					ID:          tplName,
					Path:        getString("path"),
					Title:       getString("title"),
					Description: getString("description"),
					Language:    getString("language"),
				}
			}
		}
	}

	// Parse mixins
	conf.Mixins = make(map[string]map[string]MixinDef)
	if mixinsMap := v.GetStringMap("mixins"); mixinsMap != nil {
		for lang, val := range mixinsMap {
			subMap, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			conf.Mixins[lang] = make(map[string]MixinDef)
			for mixinName, mixinVal := range subMap {
				mixinFields, ok := mixinVal.(map[string]interface{})
				if !ok {
					continue
				}

				getString := func(key string) string {
					if v, exists := mixinFields[key]; exists {
						if str, isStr := v.(string); isStr {
							return str
						}
					}
					return ""
				}

				conf.Mixins[lang][mixinName] = MixinDef{
					ID:          mixinName,
					Path:        getString("path"),
					Title:       getString("title"),
					Description: getString("description"),
					Language:    getString("language"),
				}
			}
		}
	}

	return &conf, nil
}

func loadFromRegistryJSON(fs afero.Fs, registryPath string) (*BoilerplateConfig, error) {
	data, err := afero.ReadFile(fs, registryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", registryPath, err)
	}

	var reg registrySchema
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", registryPath, err)
	}

	conf := &BoilerplateConfig{
		Environment: reg.Environment,
		Hooks:       reg.Hooks,
		Templates:   make(map[string]map[string]TemplateDef),
		Mixins:      make(map[string]map[string]MixinDef),
	}

	for tplID, tpl := range reg.Templates {
		if tpl.ID == "" {
			tpl.ID = tplID
		}
		category := inferTemplateCategory(tpl.Path, tpl.Tier)
		if conf.Templates[category] == nil {
			conf.Templates[category] = make(map[string]TemplateDef)
		}
		conf.Templates[category][tplID] = tpl
	}

	for mixinID, mixin := range reg.Mixins {
		if mixin.ID == "" {
			mixin.ID = mixinID
		}
		lang := mixin.Language
		if lang == "" {
			lang = "default"
		}
		if conf.Mixins[lang] == nil {
			conf.Mixins[lang] = make(map[string]MixinDef)
		}
		conf.Mixins[lang][mixinID] = mixin
	}

	return conf, nil
}

func inferTemplateCategory(tplPath, tier string) string {
	normalized := filepath.ToSlash(tplPath)
	parts := strings.Split(normalized, "/")
	// Expected path format: "templates/<category>/<template-name>"
	if len(parts) >= 3 && parts[0] == "templates" {
		return strings.TrimSuffix(parts[1], "s")
	}
	if tier != "" {
		return tier
	}
	return "general"
}

