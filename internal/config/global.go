package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

const (
	DefaultBoilerplateRepo = "https://github.com/ctrldown/boilerplates.git"
	DefaultLibraryRepo     = "https://github.com/ctrldown/features.git"
)

// GlobalConfig stores user-wide Ctrldown CLI settings in ~/.ctrldown/config.yaml.
// This allows standalone Linux, Windows, and macOS binaries to locate boilerplates and features
// from any working directory on the machine.
type GlobalConfig struct {
	BoilerplatePath string `mapstructure:"boilerplate_path" yaml:"boilerplate_path,omitempty"`
	LibraryPath     string `mapstructure:"library_path" yaml:"library_path,omitempty"`
	BoilerplateRepo string `mapstructure:"boilerplate_repo" yaml:"boilerplate_repo,omitempty"`
	LibraryRepo     string `mapstructure:"library_repo" yaml:"library_repo,omitempty"`
	GitHubRepo      string `mapstructure:"github_repo" yaml:"github_repo,omitempty"`
}

// GlobalHomeDir returns the root directory for global Ctrldown configuration and cached libraries.
// Respects CTRLDOWN_HOME if set, otherwise defaults to ~/.ctrldown.
func GlobalHomeDir() (string, error) {
	if custom := os.Getenv("CTRLDOWN_HOME"); custom != "" {
		return custom, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".ctrldown"), nil
}

// GlobalConfigPath returns the path to ~/.ctrldown/config.yaml.
func GlobalConfigPath() (string, error) {
	dir, err := GlobalHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// LoadGlobalConfig loads ~/.ctrldown/config.yaml, returning defaults if the file does not exist yet.
func LoadGlobalConfig(fs afero.Fs) (*GlobalConfig, error) {
	cfgPath, err := GlobalConfigPath()
	if err != nil {
		return defaultGlobalConfig(""), nil
	}

	homeDir := filepath.Dir(cfgPath)
	exists, err := afero.Exists(fs, cfgPath)
	if err != nil || !exists {
		return defaultGlobalConfig(homeDir), nil
	}

	data, err := afero.ReadFile(fs, cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read global config %s: %w", cfgPath, err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to parse global config %s: %w", cfgPath, err)
	}

	var conf GlobalConfig
	if err := v.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal global config %s: %w", cfgPath, err)
	}

	applyGlobalDefaults(&conf, homeDir)
	return &conf, nil
}

// SaveGlobalConfig persists the user-wide GlobalConfig to ~/.ctrldown/config.yaml.
func SaveGlobalConfig(fs afero.Fs, conf *GlobalConfig) error {
	cfgPath, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	homeDir := filepath.Dir(cfgPath)
	if err := fs.MkdirAll(homeDir, 0755); err != nil {
		return fmt.Errorf("failed to create global config directory %s: %w", homeDir, err)
	}

	applyGlobalDefaults(conf, homeDir)

	yamlBytes, err := yaml.Marshal(conf)
	if err != nil {
		return fmt.Errorf("failed to marshal global config: %w", err)
	}

	header := `# ========================================================
# CTRLDOWN GLOBAL CONFIGURATION (~/.ctrldown/config.yaml)
# ========================================================
`
	content := append([]byte(header), yamlBytes...)
	if err := afero.WriteFile(fs, cfgPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write global config %s: %w", cfgPath, err)
	}
	return nil
}

func defaultGlobalConfig(homeDir string) *GlobalConfig {
	conf := &GlobalConfig{}
	applyGlobalDefaults(conf, homeDir)
	return conf
}

func applyGlobalDefaults(conf *GlobalConfig, homeDir string) {
	if conf.BoilerplateRepo == "" {
		conf.BoilerplateRepo = DefaultBoilerplateRepo
	}
	if conf.LibraryRepo == "" {
		conf.LibraryRepo = DefaultLibraryRepo
	}
	if conf.GitHubRepo == "" {
		conf.GitHubRepo = GitHubRepo
	}
}

// ResolveLibraryPath resolves the Ctrldown features library path using priority:
// 1. Explicit CLI flag
// 2. CTRLDOWN_LIBRARY_PATH environment variable
// 3. Project ctrl.yaml (library_path)
// 4. Global ~/.ctrldown/config.yaml (library_path)
// 5. Local relative ../features or ./features (if existing on disk)
// 6. Global cache ~/.ctrldown/features
func ResolveLibraryPath(fs afero.Fs, explicitPath string, workDir string) string {
	if explicitPath != "" {
		return explicitPath
	}
	if envPath := os.Getenv("CTRLDOWN_LIBRARY_PATH"); envPath != "" {
		return envPath
	}
	if workDir == "" {
		workDir = "."
	}
	if ctrlConf, err := LoadCtrlConfig(fs, workDir); err == nil && ctrlConf.LibraryPath != "" {
		if exists, _ := afero.DirExists(fs, ctrlConf.LibraryPath); exists {
			return ctrlConf.LibraryPath
		}
	}
	if globalConf, err := LoadGlobalConfig(fs); err == nil && globalConf.LibraryPath != "" {
		return globalConf.LibraryPath
	}

	candidates := []string{
		"../features",
		"features",
	}
	if homeDir, err := GlobalHomeDir(); err == nil && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, "features"))
	}

	for _, c := range candidates {
		if exists, err := afero.DirExists(fs, c); err == nil && exists {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}

	if homeDir, err := GlobalHomeDir(); err == nil && homeDir != "" {
		return filepath.Join(homeDir, "features")
	}
	if abs, err := filepath.Abs("../features"); err == nil {
		return abs
	}
	return "../features"
}

// ResolveBoilerplatePath resolves the Ctrldown boilerplates directory using priority:
// 1. Explicit CLI flag
// 2. CTRLDOWN_BOILERPLATE_PATH environment variable
// 3. Project ctrl.yaml (boilerplate_path)
// 4. Global ~/.ctrldown/config.yaml (boilerplate_path)
// 5. Local relative ../boilerplates or ./boilerplates (if existing on disk)
// 6. Global cache ~/.ctrldown/boilerplates
func ResolveBoilerplatePath(fs afero.Fs, explicitPath string, workDir string) string {
	if explicitPath != "" {
		return explicitPath
	}
	if envPath := os.Getenv("CTRLDOWN_BOILERPLATE_PATH"); envPath != "" {
		return envPath
	}
	if workDir == "" {
		workDir = "."
	}
	if ctrlConf, err := LoadCtrlConfig(fs, workDir); err == nil && ctrlConf.BoilerplatePath != "" {
		if exists, _ := afero.DirExists(fs, ctrlConf.BoilerplatePath); exists {
			return ctrlConf.BoilerplatePath
		}
	}
	if globalConf, err := LoadGlobalConfig(fs); err == nil && globalConf.BoilerplatePath != "" {
		return globalConf.BoilerplatePath
	}

	candidates := []string{
		"../boilerplates",
		"boilerplates",
	}
	if homeDir, err := GlobalHomeDir(); err == nil && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, "boilerplates"))
	}

	for _, c := range candidates {
		if exists, err := afero.DirExists(fs, c); err == nil && exists {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}

	if homeDir, err := GlobalHomeDir(); err == nil && homeDir != "" {
		return filepath.Join(homeDir, "boilerplates")
	}
	if abs, err := filepath.Abs("../boilerplates"); err == nil {
		return abs
	}
	return "../boilerplates"
}
