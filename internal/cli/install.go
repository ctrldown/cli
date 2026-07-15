package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"ctrldown/cli/internal/config"
	"ctrldown/cli/internal/engine"
	"ctrldown/cli/internal/fs"
)

// NewInstallCmd creates a new instance of the install command.
func NewInstallCmd(fileSystem fs.FS) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a new component from boilerplates",
		Long:  `Guides you through selecting a component type, a template, and mixins to install in your current feature.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Load context
			ctrlConf, err := config.LoadCtrlConfig(fileSystem, ".")
			if err != nil {
				return err
			}

			bpConf, err := config.LoadBoilerplateConfig(fileSystem, ctrlConf.BoilerplatePath)
			if err != nil {
				return err
			}

			// 2. Prepare Types
			var types []string
			for t := range bpConf.Templates {
				types = append(types, t)
			}
			sort.Strings(types)

			if len(types) == 0 {
				return fmt.Errorf("no templates found in boilerplates")
			}

			var selectedType string
			typeOptions := make([]huh.Option[string], len(types))
			for i, t := range types {
				typeOptions[i] = huh.NewOption(t, t)
			}

			err = huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("What type of component do you want to install?").
						Options(typeOptions...).
						Value(&selectedType),
				),
			).Run()
			if err != nil {
				return err
			}

			// 3. Prepare Templates for selected type
			templatesMap := bpConf.Templates[selectedType]
			if len(templatesMap) == 0 {
				return fmt.Errorf("no templates available for type: %s", selectedType)
			}

			var tplNames []string
			for name := range templatesMap {
				tplNames = append(tplNames, name)
			}
			sort.Strings(tplNames)

			var selectedTemplateKey string
			tplOptions := make([]huh.Option[string], len(tplNames))
			for i, name := range tplNames {
				tplOptions[i] = huh.NewOption(templatesMap[name].Title, name)
			}

			err = huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select the base template:").
						Options(tplOptions...).
						Value(&selectedTemplateKey),
				),
			).Run()
			if err != nil {
				return err
			}

			selectedTemplate := templatesMap[selectedTemplateKey]

			// 4. Prepare Mixins for the selected language
			var selectedMixinKeys []string
			mixinsMap := bpConf.Mixins[selectedTemplate.Language]
			
			if len(mixinsMap) > 0 {
				var mixinNames []string
				for name := range mixinsMap {
					mixinNames = append(mixinNames, name)
				}
				sort.Strings(mixinNames)

				mixinOptions := make([]huh.Option[string], len(mixinNames))
				for i, name := range mixinNames {
					mixinOptions[i] = huh.NewOption(mixinsMap[name].Title, name)
				}

				err = huh.NewForm(
					huh.NewGroup(
						huh.NewMultiSelect[string]().
							Title(fmt.Sprintf("Select mixins for %s (Space to select, Enter to confirm):", selectedTemplate.Language)).
							Options(mixinOptions...).
							Value(&selectedMixinKeys),
					),
				).Run()
				if err != nil {
					return err
				}
			}

			// Gather selected Mixin definitions
			var selectedMixins []config.MixinDef
			for _, mk := range selectedMixinKeys {
				selectedMixins = append(selectedMixins, mixinsMap[mk])
			}

			// 5. Execute Installation
			action := func() {
				err = engine.InstallComponent(
					fileSystem, 
					ctrlConf.BoilerplatePath, 
					".", 
					selectedTemplate, 
					selectedMixins, 
					bpConf.Hooks,
				)
				time.Sleep(500 * time.Millisecond) // visual feedback
			}

			_ = spinner.New().
				Title("Installing component and injecting mixins...").
				Action(action).
				Run()

			if err != nil {
				return fmt.Errorf("failed to install component: %w", err)
			}

			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Bold(true)
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully installed %s!", selectedTemplate.Title)))

			return nil
		},
	}
	return cmd
}
