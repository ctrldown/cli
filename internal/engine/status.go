package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/afero"
	"ctrldown/cli/internal/config"
)

// FeatureStatusEntry describes the update and drift status of a single installed feature.
type FeatureStatusEntry struct {
	Name              string
	InstalledVersion  string
	LibraryVersion    string
	InstalledChecksum string
	LibraryChecksum   string
	LocalChecksum     string
	HasLocalDrift     bool
	State             string // "up-to-date", "outdated", "unlocked", or "missing-in-library"
}

// ProjectStatusReport summarizes schema health, filesystem drift, and feature update availability.
type ProjectStatusReport struct {
	ProjectTitle            string
	SchemaVersion           int
	LatestSchemaVersion     int
	IsSchemaOutdated        bool
	CLIVersion              string
	UntrackedDomains        []string
	MissingDomains          []string
	UntrackedBackendModules []string
	MissingBackendModules   []string
	UntrackedPackages       []string
	MissingPackages         []string
	FeatureStatuses         []FeatureStatusEntry
}

// HasArchitectureDrift returns true if the disk layout differs from ctrl.yaml.
func (r *ProjectStatusReport) HasArchitectureDrift() bool {
	return len(r.UntrackedDomains) > 0 ||
		len(r.MissingDomains) > 0 ||
		len(r.UntrackedBackendModules) > 0 ||
		len(r.MissingBackendModules) > 0 ||
		len(r.UntrackedPackages) > 0 ||
		len(r.MissingPackages) > 0
}

// ComputeDirChecksum calculates a deterministic SHA-256 fingerprint over all files in dirPath.
func ComputeDirChecksum(fs afero.Fs, dirPath string) (string, error) {
	if !dirExists(fs, dirPath) {
		return "", fmt.Errorf("directory does not exist: %s", dirPath)
	}

	var relPaths []string
	err := afero.Walk(fs, dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(relPaths)
	hasher := sha256.New()
	for _, rel := range relPaths {
		full := filepath.Join(dirPath, filepath.FromSlash(rel))
		data, err := afero.ReadFile(fs, full)
		if err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(rel + "\x00"))
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte("\x00"))
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 16 {
		sum = sum[:16]
	}
	return "sha256:" + sum, nil
}

// ComputeInstalledTargetsChecksum calculates the SHA-256 fingerprint of a feature's target slices inside a project.
func ComputeInstalledTargetsChecksum(fs afero.Fs, projectDir string, targets map[string]string) (string, error) {
	if len(targets) == 0 {
		return "", nil
	}

	type fileRecord struct {
		virtualKey string
		fullPath   string
	}
	var records []fileRecord

	sliceKeys := make([]string, 0, len(targets))
	for k := range targets {
		sliceKeys = append(sliceKeys, k)
	}
	sort.Strings(sliceKeys)

	for _, sliceRel := range sliceKeys {
		targetRel := targets[sliceRel]
		targetFull := filepath.Join(projectDir, filepath.FromSlash(targetRel))
		info, err := fs.Stat(targetFull)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			records = append(records, fileRecord{
				virtualKey: filepath.ToSlash(sliceRel),
				fullPath:   targetFull,
			})
			continue
		}
		_ = afero.Walk(fs, targetFull, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil || fi == nil || fi.IsDir() {
				return walkErr
			}
			rel, err := filepath.Rel(targetFull, path)
			if err != nil {
				return nil
			}
			records = append(records, fileRecord{
				virtualKey: filepath.ToSlash(filepath.Join(sliceRel, rel)),
				fullPath:   path,
			})
			return nil
		})
	}

	if len(records) == 0 {
		return "", nil
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].virtualKey < records[j].virtualKey
	})

	hasher := sha256.New()
	for _, rec := range records {
		data, err := afero.ReadFile(fs, rec.fullPath)
		if err != nil {
			continue
		}
		// Stripslices/ prefix so it matches ComputeDirChecksum on <featureDir>/slices
		trimmedKey := filepath.ToSlash(rec.virtualKey)
		if len(trimmedKey) > 7 && trimmedKey[:7] == "slices/" {
			trimmedKey = trimmedKey[7:]
		}
		_, _ = hasher.Write([]byte(trimmedKey + "\x00"))
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte("\x00"))
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 16 {
		sum = sum[:16]
	}
	return "sha256:" + sum, nil
}

// RecordFeatureLock records the version and content checksum of an installed feature in CtrlConfig.
func RecordFeatureLock(fs afero.Fs, ctrlConf *config.CtrlConfig, featureID string, version string, featureDir string) {
	if ctrlConf.FeatureLocks == nil {
		ctrlConf.FeatureLocks = make(map[string]config.FeatureLock)
	}
	if version == "" {
		version = "1.0.0"
	}
	checksum := "builtin"
	if featureDir != "" {
		slicesDir := filepath.Join(featureDir, "slices")
		if sum, err := ComputeDirChecksum(fs, slicesDir); err == nil && sum != "" {
			checksum = sum
		} else if sum, err := ComputeDirChecksum(fs, featureDir); err == nil && sum != "" {
			checksum = sum
		}
	}

	ctrlConf.FeatureLocks[featureID] = config.FeatureLock{
		Version:   version,
		Checksum:  checksum,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
}

// CheckProjectStatus evaluates schema version, filesystem drift, and feature updates for a project.
func CheckProjectStatus(fs afero.Fs, projectDir string, libraryDir string) (*ProjectStatusReport, error) {
	ctrlConf, err := LoadRawCtrlConfigForStatus(fs, projectDir)
	if err != nil {
		return nil, err
	}

	liveConf, err := InspectProject(fs, projectDir)
	if err != nil {
		return nil, err
	}

	schemaVer := ctrlConf.SchemaVersion
	if schemaVer == 0 {
		schemaVer = 1 // Legacy ctrl.yaml without schema_version field
	}

	report := &ProjectStatusReport{
		ProjectTitle:        ctrlConf.Project.Title,
		SchemaVersion:       schemaVer,
		LatestSchemaVersion: config.CurrentSchemaVersion,
		IsSchemaOutdated:    schemaVer < config.CurrentSchemaVersion || (ctrlConf.Project.Variant == "Project" && ctrlConf.Backend == nil && liveConf.Backend != nil),
		CLIVersion:          ctrlConf.CLIVersion,
	}

	// 1. Compare Domains
	var confDomains, liveDomains []string
	for _, d := range ctrlConf.Domains {
		confDomains = append(confDomains, d.Name)
	}
	for _, d := range liveConf.Domains {
		liveDomains = append(liveDomains, d.Name)
	}
	report.UntrackedDomains, report.MissingDomains = diffStringSlices(confDomains, liveDomains)

	// 2. Compare Go Echo Backend Modules
	var confMods, liveMods []string
	if ctrlConf.Backend != nil {
		confMods = ctrlConf.Backend.Modules
	}
	if liveConf.Backend != nil {
		liveMods = liveConf.Backend.Modules
	}
	report.UntrackedBackendModules, report.MissingBackendModules = diffStringSlices(confMods, liveMods)

	// 3. Compare Workspace Packages
	var confPkgs, livePkgs []string
	for _, p := range ctrlConf.Packages {
		confPkgs = append(confPkgs, p.Name)
	}
	for _, p := range liveConf.Packages {
		livePkgs = append(livePkgs, p.Name)
	}
	report.UntrackedPackages, report.MissingPackages = diffStringSlices(confPkgs, livePkgs)

	// 4. Evaluate Installed Features against Feature Library
	libDir := libraryDir
	if libDir == "" && ctrlConf.LibraryPath != "" {
		libDir = ctrlConf.LibraryPath
	}

	for _, featID := range ctrlConf.Features {
		entry := FeatureStatusEntry{
			Name: featID,
		}
		lock, hasLock := ctrlConf.FeatureLocks[featID]
		if hasLock {
			entry.InstalledVersion = lock.Version
			entry.InstalledChecksum = lock.Checksum
		}

		featLibDir := filepath.Join(libDir, featID)
		if libDir == "" || !dirExists(fs, featLibDir) {
			if hasLock {
				entry.State = "up-to-date"
			} else {
				entry.State = "unlocked"
			}
			report.FeatureStatuses = append(report.FeatureStatuses, entry)
			continue
		}

		manifest, mErr := LoadFeatureManifest(fs, featLibDir)
		if mErr == nil {
			entry.LibraryVersion = manifest.Version
			if entry.LibraryVersion == "" {
				entry.LibraryVersion = "1.0.0"
			}
			if len(manifest.Targets) > 0 {
				entry.LocalChecksum, _ = ComputeInstalledTargetsChecksum(fs, projectDir, manifest.Targets)
			}
		}

		slicesDir := filepath.Join(featLibDir, "slices")
		if sum, cErr := ComputeDirChecksum(fs, slicesDir); cErr == nil {
			entry.LibraryChecksum = sum
		}

		if !hasLock {
			entry.State = "unlocked"
		} else {
			if entry.LocalChecksum != "" && entry.InstalledChecksum != "" && entry.InstalledChecksum != "builtin" && entry.LocalChecksum != entry.InstalledChecksum {
				entry.HasLocalDrift = true
			}
			if (entry.LibraryVersion != "" && entry.InstalledVersion != entry.LibraryVersion) ||
				(entry.LibraryChecksum != "" && entry.InstalledChecksum != "builtin" && entry.InstalledChecksum != entry.LibraryChecksum) {
				entry.State = "outdated"
			} else {
				entry.State = "up-to-date"
			}
		}

		report.FeatureStatuses = append(report.FeatureStatuses, entry)
	}

	return report, nil
}

// UpgradeProject migrates ctrl.yaml to the latest schema version, syncs filesystem drift,
// and optionally updates outdated library features.
func UpgradeProject(fs afero.Fs, projectDir string, libraryDir string, upgradeFeatures bool) (*ProjectStatusReport, error) {
	ctrlConf, err := LoadRawCtrlConfigForStatus(fs, projectDir)
	if err != nil {
		// If ctrl.yaml does not exist yet, adopt the project first
		ctrlConf, err = InspectProject(fs, projectDir)
		if err != nil {
			return nil, err
		}
	}

	liveConf, err := InspectProject(fs, projectDir)
	if err != nil {
		return nil, err
	}

	// Upgrade schema and CLI version
	ctrlConf.SchemaVersion = config.CurrentSchemaVersion
	ctrlConf.CLIVersion = config.CurrentCLIVersion
	if ctrlConf.Project.Architecture == "" {
		ctrlConf.Project.Architecture = liveConf.Project.Architecture
	}
	if ctrlConf.Project.Title == "" {
		ctrlConf.Project.Title = liveConf.Project.Title
	}
	if ctrlConf.Project.Variant == "" {
		ctrlConf.Project.Variant = liveConf.Project.Variant
	}
	if libraryDir != "" && ctrlConf.LibraryPath == "" {
		ctrlConf.LibraryPath = libraryDir
	}

	// Sync Domains, Backend Modules, and Packages with live filesystem
	ctrlConf.Domains = liveConf.Domains
	if liveConf.Backend != nil {
		if ctrlConf.Backend != nil {
			liveConf.Backend.Auth = ctrlConf.Backend.Auth
			if ctrlConf.Backend.Database != "" {
				liveConf.Backend.Database = ctrlConf.Backend.Database
			}
		}
		ctrlConf.Backend = liveConf.Backend
	}
	ctrlConf.Packages = liveConf.Packages

	libDir := libraryDir
	if libDir == "" {
		libDir = ctrlConf.LibraryPath
	}

	if ctrlConf.FeatureLocks == nil {
		ctrlConf.FeatureLocks = make(map[string]config.FeatureLock)
	}

	if err := config.SaveCtrlConfig(fs, projectDir, ctrlConf); err != nil {
		return nil, err
	}

	// Lock unlocked features or upgrade outdated features
	for _, featID := range ctrlConf.Features {
		featLibDir := filepath.Join(libDir, featID)
		_, hasLock := ctrlConf.FeatureLocks[featID]

		if upgradeFeatures && dirExists(fs, featLibDir) {
			if err := IntegrateFeatureToMonolith(fs, IntegrateFeatureOptions{
				ProjectDir:  projectDir,
				FeatureName: featID,
				SourceDir:   featLibDir,
				LibraryDir:  libDir,
			}); err != nil {
				return nil, fmt.Errorf("failed to upgrade feature %s: %w", featID, err)
			}
			if reloaded, err := config.LoadCtrlConfig(fs, projectDir); err == nil {
				ctrlConf = reloaded
			}
		} else if !hasLock {
			ver := "1.0.0"
			if m, err := LoadFeatureManifest(fs, featLibDir); err == nil && m.Version != "" {
				ver = m.Version
			}
			RecordFeatureLock(fs, ctrlConf, featID, ver, featLibDir)
		}
	}

	if err := config.SaveCtrlConfig(fs, projectDir, ctrlConf); err != nil {
		return nil, err
	}

	return CheckProjectStatus(fs, projectDir, libDir)
}

// LoadRawCtrlConfigForStatus reads ctrl.yaml without overwriting SchemaVersion=0 so legacy v1 files can be detected.
func LoadRawCtrlConfigForStatus(fs afero.Fs, workDir string) (*config.CtrlConfig, error) {
	return config.LoadCtrlConfig(fs, workDir)
}

func diffStringSlices(recorded, live []string) (untracked []string, missing []string) {
	recSet := make(map[string]bool, len(recorded))
	liveSet := make(map[string]bool, len(live))
	for _, s := range recorded {
		recSet[s] = true
	}
	for _, s := range live {
		liveSet[s] = true
		if !recSet[s] {
			untracked = append(untracked, s)
		}
	}
	for _, s := range recorded {
		if !liveSet[s] {
			missing = append(missing, s)
		}
	}
	sort.Strings(untracked)
	sort.Strings(missing)
	return
}
