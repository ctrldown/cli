package config

import (
	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

type CtrlConfig struct {
	BoilerplatePath string        `mapstructure:"boilerplate_path"`
	Project         ProjectConfig `mapstructure:"project"`
}

type ProjectConfig struct {
	Variant   string `mapstructure:"variant"`
	Title     string `mapstructure:"title"`
	CreatedAt string `mapstructure:"created_at"`
}

type TemplateDef struct {
	Path        string `mapstructure:"path"`
	Title       string `mapstructure:"title"`
	Description string `mapstructure:"description"`
	Language    string `mapstructure:"language"`
}

type MixinDef struct {
	Path        string `mapstructure:"path"`
	Title       string `mapstructure:"title"`
	Description string `mapstructure:"description"`
	Language    string `mapstructure:"language"`
}

type BoilerplateConfig struct {
	Environment map[string]string         `mapstructure:"environment"`
	Hooks       map[string][]string       `mapstructure:"hooks"`
	Templates   map[string]map[string]TemplateDef
	Mixins      map[string]map[string]MixinDef
}

func LoadCtrlConfig(fs afero.Fs, workDir string) (*CtrlConfig, error) {
	v := viper.New()
	v.SetFs(fs)
	v.SetConfigName("ctrl")
	v.SetConfigType("yaml")
	v.AddConfigPath(workDir)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read ctrl.yaml (are you in a ctrldown initialized project?): %w", err)
	}

	var conf CtrlConfig
	if err := v.Unmarshal(&conf); err != nil {
		return nil, err
	}
	return &conf, nil
}

func LoadBoilerplateConfig(fs afero.Fs, boilerplatePath string) (*BoilerplateConfig, error) {
	v := viper.New()
	v.SetFs(fs)
	v.SetConfigName("cli")
	v.SetConfigType("toml")
	v.AddConfigPath(boilerplatePath)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read cli.toml in %s: %w", boilerplatePath, err)
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
				
				// Helper to safely extract strings
				getString := func(key string) string {
					if v, exists := tplFields[key]; exists {
						if str, isStr := v.(string); isStr {
							return str
						}
					}
					return ""
				}

				conf.Templates[category][tplName] = TemplateDef{
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
