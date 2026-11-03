package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewIntegrateCmd creates a new instance of the integrate command.
func NewIntegrateCmd(fileSystem fs.FS) *cobra.Command {
	var (
		mode            string
		projectDir      string
		boilerplatePath string
		libraryPath     string
		featureName     string
		sourceDir       string
		outputDir       string
		targetDomain    string
		withMigrate     bool
		withExampleApp  bool
		exampleAppName  string
		dryRun          bool
		nonInteractive  bool
	)

	cmd := &cobra.Command{
		Use:   "integrate [project|extract|feature|ctrlweb]",
		Short: "Integrate an existing project, extract a modular package, or wire a feature/ctrlweb into a monolith",
		Long: `Provides integration workflows for monolithic and microfeature architectures:
  • project : Scans an existing monolithic repository (e.g. Etkilibagis) and generates ctrl.yaml
  • extract : Extracts a package or full-stack feature slice into the Ctrldown features library
  • feature : Wires a library feature or new Go Echo module into the monolithic project's AST and workspace
  • ctrlweb : Integrates the Ctrlweb visual builder, Go Echo delivery service, @workspace/ui bridge, and optional minimal Next.js example app`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && mode == "" {
				mode = args[0]
			}
			if projectDir == "" {
				projectDir = "."
			}
			resolvedLibPath := resolveDefaultLibraryPath(fileSystem, libraryPath)

			if !nonInteractive && mode == "" {
				err := huh.NewForm(
					huh.NewGroup(
						huh.NewSelect[string]().
							Title("Select integration workflow:").
							Options(
								huh.NewOption("1. Adopt Existing Project (Scan monorepo & generate ctrl.yaml)", "project"),
								huh.NewOption("2. Extract Feature/Package (Export module to Ctrldown library)", "extract"),
								huh.NewOption("3. Integrate Feature into Monolith (Install from library or scaffold Go Echo module)", "feature"),
								huh.NewOption("4. Integrate Ctrlweb Pipeline (Builder + Go Echo Service + @workspace/ui + Example App)", "ctrlweb"),
							).
							Value(&mode),
					),
				).Run()
				if err != nil {
					return err
				}
			}

			switch mode {
			case "project", "adopt":
				return runIntegrateProject(fileSystem, projectDir, boilerplatePath, dryRun, nonInteractive)
			case "extract":
				return runIntegrateExtract(fileSystem, projectDir, resolvedLibPath, featureName, outputDir, nonInteractive)
			case "feature", "module":
				return runIntegrateFeature(fileSystem, projectDir, resolvedLibPath, featureName, sourceDir, targetDomain, withMigrate, withExampleApp, exampleAppName, nonInteractive)
			case "ctrlweb":
				return runIntegrateCtrlweb(fileSystem, projectDir, withExampleApp, exampleAppName, nonInteractive)
			default:
				return fmt.Errorf("unknown integration mode %q (expected 'project', 'extract', 'feature', or 'ctrlweb')", mode)
			}
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "Integration mode: 'project', 'extract', 'feature', or 'ctrlweb'")
	cmd.Flags().StringVar(&projectDir, "dir", ".", "Target project directory")
	cmd.Flags().StringVar(&boilerplatePath, "boilerplate", "", "Optional boilerplate repository path")
	cmd.Flags().StringVar(&libraryPath, "library", "", "Optional Ctrldown features library directory (defaults to ../features)")
	cmd.Flags().StringVar(&featureName, "name", "", "Feature or module name (for 'extract' or 'feature' mode)")
	cmd.Flags().StringVar(&sourceDir, "source", "", "Source directory of the feature to integrate")
	cmd.Flags().StringVar(&outputDir, "output", "", "Output directory when extracting a feature")
	cmd.Flags().StringVar(&targetDomain, "domain", "", "Target frontend domain to link workspace package into")
	cmd.Flags().BoolVar(&withMigrate, "migrate", false, "Include GORM AutoMigrate wiring in Go Echo main.go")
	cmd.Flags().BoolVar(&withExampleApp, "with-example-app", false, "Scaffold an ultra-minimal Next.js example consumer app when integrating ctrlweb")
	cmd.Flags().StringVar(&exampleAppName, "example-app-name", "ctrlweb-example", "Domain folder name for the minimal Next.js example app")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Inspect and print project summary without writing files")
	cmd.Flags().BoolVar(&nonInteractive, "yes", false, "Skip interactive prompts")

	return cmd
}

func runIntegrateCtrlweb(
	fileSystem fs.FS,
	projectDir string,
	withExampleApp bool,
	exampleAppName string,
	nonInteractive bool,
) error {
	withBackend := true
	withBuilder := true

	if !nonInteractive {
		if exampleAppName == "" {
			exampleAppName = "ctrlweb-example"
		}
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Include dedicated Go Echo delivery service (domains/api/internal/ctrlweb)?").
					Value(&withBackend),
				huh.NewConfirm().
					Title("Include Ctrlweb visual builder domain (domains/ctrlweb) with @workspace/ui bridge?").
					Value(&withBuilder),
				huh.NewConfirm().
					Title("Create ultra-minimal Next.js example consumer app (renders published Ctrlweb pages)?").
					Value(&withExampleApp),
			),
		).Run()
		if err != nil {
			return err
		}

		if withExampleApp {
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Enter domain folder name for the minimal Next.js example app:").
						Placeholder("ctrlweb-example").
						Value(&exampleAppName),
				),
			).Run()
			if err != nil {
				return err
			}
		}
	}

	opts := engine.CtrlwebIntegrationOptions{
		ProjectDir:         projectDir,
		WithBackendService: withBackend,
		WithBuilderDomain:  withBuilder,
		WithExampleApp:     withExampleApp,
		ExampleAppName:     exampleAppName,
	}

	if err := engine.IntegrateCtrlweb(fileSystem, opts); err != nil {
		return fmt.Errorf("failed to integrate ctrlweb: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	fmt.Println(successStyle.Render("✓ Successfully integrated Ctrlweb pipeline into project!"))
	fmt.Println(infoStyle.Render("  • @workspace/ui Bridge: packages/ui/src/ctrlweb-registry.tsx"))
	if withBackend {
		fmt.Println(infoStyle.Render("  • Go Echo Service: domains/api/internal/ctrlweb (GET /api/v1/ctrlweb/pages/:slug)"))
	}
	if withBuilder {
		fmt.Println(infoStyle.Render("  • Visual Builder Domain: domains/ctrlweb (Ory Kratos/Keto + @workspace/ui)"))
	}
	if withExampleApp {
		fmt.Println(infoStyle.Render(fmt.Sprintf("  • Minimal Next.js Example App: domains/%s (src/app/[[...slug]]/page.tsx)", opts.ExampleAppName)))
	}
	return nil
}

func runIntegrateProject(
	fileSystem fs.FS,
	projectDir string,
	boilerplatePath string,
	dryRun bool,
	nonInteractive bool,
) error {
	if !nonInteractive && projectDir == "." {
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter the existing project directory path:").
					Placeholder(".").
					Value(&projectDir).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("project directory is required")
						}
						return nil
					}),
			),
		).Run()
		if err != nil {
			return err
		}
	}

	var conf *config.CtrlConfig
	var err error

	if dryRun {
		conf, err = engine.InspectProject(fileSystem, projectDir)
	} else {
		action := func() {
			conf, err = engine.AdoptProject(fileSystem, projectDir, boilerplatePath)
			time.Sleep(300 * time.Millisecond)
		}
		if nonInteractive {
			conf, err = engine.AdoptProject(fileSystem, projectDir, boilerplatePath)
		} else {
			_ = spinner.New().
				Title("Inspecting project architecture and generating ctrl.yaml...").
				Action(action).
				Run()
		}
	}

	if err != nil {
		return fmt.Errorf("failed to integrate project: %w", err)
	}

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

	absDir, pathErr := filepath.Abs(projectDir)
	if pathErr != nil {
		absDir = projectDir
	}

	if dryRun {
		fmt.Println(headerStyle.Render(fmt.Sprintf("✓ Inspected project '%s' (%s) [DRY-RUN]", conf.Project.Title, absDir)))
	} else {
		fmt.Println(headerStyle.Render(fmt.Sprintf("✓ Integrated project '%s' and wrote ctrl.yaml in %s", conf.Project.Title, absDir)))
	}

	fmt.Printf("  %s %s\n", labelStyle.Render("Architecture:"), conf.Project.Architecture)
	if conf.Backend != nil {
		fmt.Printf("  %s %s (%s) — Modules: [%s]\n",
			labelStyle.Render("Backend:"),
			conf.Backend.Framework,
			conf.Backend.Path,
			strings.Join(conf.Backend.Modules, ", "),
		)
	}
	if len(conf.Domains) > 0 {
		var domNames []string
		for _, d := range conf.Domains {
			domNames = append(domNames, fmt.Sprintf("%s (%s)", d.Name, d.Type))
		}
		fmt.Printf("  %s %s\n", labelStyle.Render("Domains:"), strings.Join(domNames, ", "))
	}
	if len(conf.Packages) > 0 {
		var pkgNames []string
		for _, p := range conf.Packages {
			pkgNames = append(pkgNames, p.Name)
		}
		fmt.Printf("  %s %s\n", labelStyle.Render("Packages:"), strings.Join(pkgNames, ", "))
	}

	return nil
}

func runIntegrateExtract(
	fileSystem fs.FS,
	projectDir string,
	libraryPath string,
	featureName string,
	outputDir string,
	nonInteractive bool,
) error {
	// Special case: seeding the clean ctrlweb pipeline into the library without modifying any source project
	if strings.EqualFold(featureName, "ctrlweb") {
		targetLib := libraryPath
		if outputDir != "" {
			targetLib = filepath.Dir(outputDir)
		}
		if err := engine.SeedCtrlwebFeatureLibrary(fileSystem, targetLib); err != nil {
			return fmt.Errorf("failed to seed ctrlweb feature in library: %w", err)
		}
		successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
		fmt.Println(successStyle.Render(fmt.Sprintf("✓ Prepared 'ctrlweb' feature pipeline in %s", filepath.Join(targetLib, "ctrlweb"))))
		return nil
	}

	conf, err := engine.InspectProject(fileSystem, projectDir)
	if err != nil {
		return fmt.Errorf("failed to inspect source project: %w", err)
	}

	var candidateOptions []huh.Option[string]
	sliceMap := make(map[string]map[string]string)
	categoryMap := make(map[string]string)

	// Offer full-stack composite presets first if detected
	if _, ok := engine.BuildCompositeExtractOptions(fileSystem, projectDir, "auth", ""); ok {
		candidateOptions = append(candidateOptions, huh.NewOption("Full-Stack Feature: auth (Ory Kratos/Keto + Identity + Dashboard Auth/Access)", "composite:auth"))
	}
	if _, ok := engine.BuildCompositeExtractOptions(fileSystem, projectDir, "bloub", ""); ok {
		candidateOptions = append(candidateOptions, huh.NewOption("Feature Package: bloub (@workspace/bloub SVG Avatar & Studio)", "composite:bloub"))
	}

	for _, pkg := range conf.Packages {
		key := "pkg:" + pkg.Name
		label := fmt.Sprintf("Package: %s (%s)", pkg.Name, pkg.Path)
		candidateOptions = append(candidateOptions, huh.NewOption(label, key))
		sliceMap[key] = map[string]string{"package": pkg.Path}
		categoryMap[key] = "package"
	}

	if conf.Backend != nil {
		for _, mod := range conf.Backend.Modules {
			key := "mod:" + mod
			modRel := filepath.ToSlash(filepath.Join(conf.Backend.Path, "internal", mod))
			label := fmt.Sprintf("Go Echo Module: %s (%s)", mod, modRel)
			candidateOptions = append(candidateOptions, huh.NewOption(label, key))
			sliceMap[key] = map[string]string{"backend": modRel}
			categoryMap[key] = "backend-module"
		}
	}

	var selectedKey string
	if !nonInteractive {
		if len(candidateOptions) == 0 {
			return fmt.Errorf("no extractable packages or backend modules found in %s", projectDir)
		}
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select a module or package from the project to extract:").
					Options(candidateOptions...).
					Value(&selectedKey),
				huh.NewInput().
					Title("Enter the target output directory (leave empty for Ctrldown features library):").
					Placeholder(filepath.Join(libraryPath, "<feature-name>")).
					Value(&outputDir),
			),
		).Run()
		if err != nil {
			return err
		}
	} else {
		if featureName == "" {
			return fmt.Errorf("--name is required in non-interactive extract mode")
		}
		if _, ok := engine.BuildCompositeExtractOptions(fileSystem, projectDir, featureName, ""); ok {
			selectedKey = "composite:" + strings.ToLower(featureName)
		} else if _, ok := sliceMap["mod:"+featureName]; ok {
			selectedKey = "mod:" + featureName
		} else if _, ok := sliceMap["pkg:"+featureName]; ok {
			selectedKey = "pkg:" + featureName
		} else if _, ok := sliceMap["pkg:@workspace/"+featureName]; ok {
			selectedKey = "pkg:@workspace/" + featureName
		} else {
			return fmt.Errorf("feature or package %q not found in project", featureName)
		}
	}

	cleanName := featureName
	if cleanName == "" {
		cleanName = strings.TrimPrefix(selectedKey, "composite:")
		cleanName = strings.TrimPrefix(cleanName, "mod:")
		cleanName = strings.TrimPrefix(cleanName, "pkg:@workspace/")
		cleanName = strings.TrimPrefix(cleanName, "pkg:")
	}
	if outputDir == "" {
		outputDir = filepath.Join(libraryPath, cleanName)
	}

	var opts engine.ExtractOptions
	if strings.HasPrefix(selectedKey, "composite:") {
		compositeName := strings.TrimPrefix(selectedKey, "composite:")
		var ok bool
		opts, ok = engine.BuildCompositeExtractOptions(fileSystem, projectDir, compositeName, outputDir)
		if !ok {
			return fmt.Errorf("failed to build composite extraction recipe for %s", compositeName)
		}
	} else {
		opts = engine.ExtractOptions{
			ProjectDir:  projectDir,
			FeatureName: cleanName,
			Title:       cleanName,
			Description: fmt.Sprintf("Extracted %s from %s", cleanName, conf.Project.Title),
			Category:    categoryMap[selectedKey],
			SlicePaths:  sliceMap[selectedKey],
			Debrand:     true,
			OutputDir:   outputDir,
		}
	}

	if err := engine.ExtractFeature(fileSystem, opts); err != nil {
		return fmt.Errorf("failed to extract feature: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Extracted '%s' (%d slice(s)) into %s", opts.FeatureName, len(opts.SlicePaths), outputDir)))
	return nil
}

func runIntegrateFeature(
	fileSystem fs.FS,
	projectDir string,
	libraryPath string,
	featureName string,
	sourceDir string,
	targetDomain string,
	withMigrate bool,
	withExampleApp bool,
	exampleAppName string,
	nonInteractive bool,
) error {
	libFeatures, _ := engine.DiscoverLibraryFeatures(fileSystem, libraryPath)
	creds, _ := auth.LoadCredentials(fileSystem)
	scaffoldNew := false

	if !nonInteractive {
		var sourceMode string
		modeOptions := []huh.Option[string]{}
		if len(libFeatures) > 0 {
			modeOptions = append(modeOptions, huh.NewOption(fmt.Sprintf("Install from Ctrldown Microfeature Catalog (%d available)", len(libFeatures)), "library"))
		}
		modeOptions = append(modeOptions,
			huh.NewOption("Scaffold a new Go Echo module into domains/api/internal/<name>", "scaffold"),
			huh.NewOption("Import from a custom feature directory", "import"),
		)

		err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("How do you want to integrate a feature?").
					Options(modeOptions...).
					Value(&sourceMode),
			),
		).Run()
		if err != nil {
			return err
		}

		switch sourceMode {
		case "library":
			featOptions := make([]huh.Option[string], 0, len(libFeatures))
			for _, f := range libFeatures {
				badge := "🔓"
				if !auth.IsEntitled(creds, f.ID, f.RequiredTier) {
					badge = fmt.Sprintf("🔒 [Locked: %s]", f.RequiredTier)
				}
				label := fmt.Sprintf("%s %s — %s [%s]", badge, f.ID, f.Title, f.Category)
				featOptions = append(featOptions, huh.NewOption(label, f.ID))
			}
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select a microfeature from the Ctrldown Catalog:").
						Options(featOptions...).
						Value(&featureName),
				),
			).Run()
			if err != nil {
				return err
			}

			for _, f := range libFeatures {
				if f.ID == featureName && !auth.IsEntitled(creds, f.ID, f.RequiredTier) {
					return fmt.Errorf("🔒 microfeature %q is locked (requires %s tier). Run 'ctrldown login' with an entitled account to unlock and pull it", f.ID, f.RequiredTier)
				}
			}

			if strings.EqualFold(featureName, "ctrlweb") {
				return runIntegrateCtrlweb(fileSystem, projectDir, withExampleApp, exampleAppName, false)
			}
		case "import":
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Enter the source feature directory path:").
						Value(&sourceDir).
						Validate(func(s string) error {
							if strings.TrimSpace(s) == "" {
								return fmt.Errorf("source directory is required")
							}
							return nil
						}),
				),
			).Run()
			if err != nil {
				return err
			}
		case "scaffold":
			scaffoldNew = true
			err = huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Enter the new Go Echo module name (e.g. 'notifications', 'billing'):").
						Value(&featureName).
						Validate(func(s string) error {
							if strings.TrimSpace(s) == "" {
								return fmt.Errorf("module name is required")
							}
							return nil
						}),
					huh.NewConfirm().
						Title("Inject GORM AutoMigrate into domains/api/cmd/api/main.go?").
						Value(&withMigrate),
				),
			).Run()
			if err != nil {
				return err
			}
		}
	} else {
		if sourceDir == "" && !strings.EqualFold(featureName, "ctrlweb") {
			inLibrary := false
			for _, f := range libFeatures {
				if f.ID == featureName {
					if !auth.IsEntitled(creds, f.ID, f.RequiredTier) {
						return fmt.Errorf("🔒 microfeature %q is locked (requires %s tier). Run 'ctrldown login' with an entitled account to unlock and pull it", f.ID, f.RequiredTier)
					}
					inLibrary = true
					break
				}
			}
			if !inLibrary {
				scaffoldNew = true
			}
		}
	}

	opts := engine.IntegrateFeatureOptions{
		ProjectDir:      projectDir,
		FeatureName:     featureName,
		SourceDir:       sourceDir,
		LibraryDir:      libraryPath,
		ScaffoldEchoMod: scaffoldNew,
		WithAutoMigrate: withMigrate,
		WithExampleApp:  withExampleApp,
		ExampleAppName:  exampleAppName,
		TargetDomain:    targetDomain,
	}

	if err := engine.IntegrateFeatureToMonolith(fileSystem, opts); err != nil {
		return fmt.Errorf("failed to integrate feature '%s': %w", featureName, err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully integrated feature '%s' into monolithic project!", featureName)))
	return nil
}
